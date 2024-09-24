package internal

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type Proxy struct {
	listener       net.Listener
	wg             sync.WaitGroup
	ctx            context.Context
	historyService *HistoryService
}

func NewProxy() *Proxy {
	return &Proxy{
		historyService: InitHistoryService(),
	}
}

func (p *Proxy) ListenAndServe() {
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatalf("Ошибка запуска прокси сервера: %s", err)
	}
	go p.historyService.ListenAndServe()
	p.listener = listener
	p.wg = sync.WaitGroup{}
	ctx, cancelFunc := context.WithCancel(context.Background())
	p.ctx = ctx

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		cancelFunc()
		p.listener.Close()
		p.historyService.Close()
	}()

	log.Println("Запуск прокси сервера на порту 8080")
	p.Serve()
	// ждем, пока не обработаем все подключения
	p.wg.Wait()
}

func (p *Proxy) Serve() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.ctx.Done():
				return
			default:
				log.Printf("Ошибка приёма входящего соединения: %s\n", err)
			}
		}
		p.wg.Add(1)
		go func() {
			err := p.handleConn(conn)
			if err != nil {
				log.Printf("Произошла ошибка: %s", err)
			}
			p.wg.Done()
		}()
	}
}

func (p *Proxy) handleConn(conn net.Conn) error {
	defer conn.Close()
	// Чтение первого запроса клиента
	request, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return fmt.Errorf("ошибка чтения запроса: %s", err)
	}

	// Если это HTTPS-запрос (CONNECT), то обрабатываем его отдельно
	if request.Method == http.MethodConnect {
		err = p.handleHTTPSConnect(conn, request)
	} else {
		// Обычная обработка HTTP-запросов
		err = p.handleHTTPRequest(conn, request, 80)
	}
	return err
}

func (p *Proxy) getTLSConfig(host string) (*tls.Config, error) {
	// если сертификат уже сгенерирован, то используем его, иначе - генерируем и сохраняем
	cert, err := p.historyService.GetCertificate(host)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Ошибка получения сертификата: %s", err))
	}

	return &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}, nil
}

func (p *Proxy) handleHTTPSConnect(conn net.Conn, req *http.Request) error {
	// туннель установлен
	_, err := fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	if err != nil {
		return fmt.Errorf("ошибка отправки подтверждения CONNECT: %s", err)
	}

	// Установка TLS-туннеля
	tlsCfg, err := p.getTLSConfig(req.Host)
	if err != nil {
		return fmt.Errorf("ошибка получения TLS-конфигурации: %s", err)
	}
	tlsConn := tls.Server(conn, tlsCfg)
	defer tlsConn.Close()

	// Чтение расшифрованного трафика
	for {
		request, err := http.ReadRequest(bufio.NewReader(tlsConn))
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("ошибка чтения HTTPS-запроса: %s", err)
		}
		err = p.handleHTTPRequest(tlsConn, request, 443)
		if err != nil {
			return fmt.Errorf("ошибка обработки HTTPS-запроса: %s", err)
		}
	}

	return nil
}

func (p *Proxy) handleHTTPRequest(conn net.Conn, request *http.Request, port int) error {
	log.Println(request.Method, request.URL)
	request.Header.Del("Proxy-Connection")
	var err error
	var dial net.Conn
	if port == 443 {
		tlsCfg, err := p.getTLSConfig(request.URL.Host)
		if err != nil {
			return fmt.Errorf("ошибка получения TLS-конфигурации: %s", err)
		}
		dial, err = tls.Dial("tcp", fmt.Sprintf("%s:%d", request.Host, port), tlsCfg)
	} else {
		dial, err = net.Dial("tcp", fmt.Sprintf("%s:%d", request.Host, port))
	}
	if err != nil {
		return fmt.Errorf("ошибка подключения к хосту: %s", err)
	}
	response, err := p.sendRequest(dial, request)
	if err != nil {
		return fmt.Errorf("ошибка отправки запроса: %s", err)
	}

	// сохраняем историю запроса
	_, err = p.historyService.AddHistory(request, response)
	if err != nil {
		log.Printf("Ошибка сохранения истории запроса: %s", err)
	}

	// отправляем ответ клиенту
	err = response.Write(conn)
	if err != nil {
		return fmt.Errorf("ошибка отправки ответа клиенту: %s", err)
	}
	return nil
}

func (p *Proxy) sendRequest(dial net.Conn, req *http.Request) (*http.Response, error) {
	// отправка запроса
	err := req.Write(dial)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Ошибка отправки запроса: %s", err))
	}

	// чтение ответа
	response, err := http.ReadResponse(bufio.NewReader(dial), req)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Ошибка чтения ответа: %s", err))
	}
	return response, nil
}
