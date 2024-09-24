package delivery

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/blackHATred/mitm_proxy/internal/usecase"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
)

type Proxy struct {
	listener       net.Listener
	wg             sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
	historyUsecase usecase.HistoryUsecase
}

func NewProxy(h usecase.HistoryUsecase) *Proxy {
	p := &Proxy{}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.historyUsecase = h
	p.wg = sync.WaitGroup{}
	return p
}

func (p *Proxy) StartProxyServer(wg *sync.WaitGroup, addr string) error {
	config := net.ListenConfig{
		Control:   nil,
		KeepAlive: 0,
	}
	listener, err := config.Listen(p.ctx, "tcp", addr)
	if err != nil {
		return err
	}
	p.listener = listener

	go func() {
		defer wg.Done()

		if err := p.ListenAndServe(); !errors.Is(err, net.ErrClosed) {
			log.Fatalf("ListenAndServe(): %v", err)
		}
	}()

	log.Printf("Прокси-сервер запущен на %s", addr)
	return nil
}

func (p *Proxy) ListenAndServe() error {
Listen:
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				// Прокси был завершен, выходим
				log.Printf("Прокси-сервер завершен\n")
				break Listen
			}
			log.Printf("Ошибка приёма входящего соединения: %v\n", err)
			continue
		}
		p.wg.Add(1)
		go func() {
			err := p.handleConn(conn)
			if err != nil {
				log.Printf("Произошла ошибка: %v\n", err)
			}
			p.wg.Done()
		}()
	}
	// ждем, пока не обработаем все соединения
	p.wg.Wait()
	return net.ErrClosed
}

func (p *Proxy) Shutdown(ctx context.Context) error {
	p.cancel()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	// смотрим, что произошло раньше: завершение всех соединений или завершение контекста
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
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
	cert, err := p.historyUsecase.GetCertificate(host)
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
	err = p.historyUsecase.AddHistory(request, response)
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
