package service

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/blackHATred/mitm_proxy/internal/usecase"
	"io"
	"log"
	"net"
	"net/http"
)

type Proxy struct {
	historyUsecase usecase.HistoryUsecase
}

func NewProxyService(historyUC usecase.HistoryUsecase) usecase.ProxyUsecase {
	return Proxy{
		historyUsecase: historyUC,
	}
}

func (p Proxy) HandleConn(conn net.Conn) error {
	defer conn.Close()
	// чтение первого запроса клиента
	request, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return fmt.Errorf("ошибка чтения запроса: %s", err)
	}

	// если это HTTPS-запрос (CONNECT), то обрабатываем его отдельно
	if request.Method == http.MethodConnect {
		return p.HandleHTTPSConnect(conn, request)
	}
	return p.HandleHTTPRequest(conn, request, nil)
}

func (p Proxy) GetTLSConfig(host string) (*tls.Config, error) {
	cert, err := p.historyUsecase.GetCertificate(host)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения сертификата: %s", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{*cert},
		ServerName:   host,
	}, nil
}

func (p Proxy) HandleHTTPSConnect(conn net.Conn, req *http.Request) error {
	// туннель установлен
	_, err := fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	if err != nil {
		return fmt.Errorf("ошибка отправки подтверждения CONNECT: %s", err)
	}

	// установка TLS-туннеля
	tlsCfg, err := p.GetTLSConfig(req.URL.Hostname())
	if err != nil {
		return fmt.Errorf("ошибка получения TLS-конфигурации: %s", err)
	}
	tlsConn := tls.Server(conn, tlsCfg)
	defer tlsConn.Close()

	// чтение трафика
	for {
		request, err := http.ReadRequest(bufio.NewReader(tlsConn))
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("ошибка чтения HTTPS-запроса: %s", err)
		}
		err = p.HandleHTTPRequest(tlsConn, request, tlsCfg)
		if err != nil {
			return fmt.Errorf("ошибка обработки HTTPS-запроса: %s", err)
		}
	}

	return nil
}

func (p Proxy) HandleHTTPRequest(conn net.Conn, request *http.Request, tlsCfg *tls.Config) error {
	log.Println(request.Method, request.Host, request.RequestURI)
	request.Header.Del("Proxy-Connection")
	request.Header.Del("Accept-Encoding")
	var err error
	var dial net.Conn
	if tlsCfg != nil {
		dial, err = tls.Dial("tcp", fmt.Sprintf("%s:%d", request.Host, 443), tlsCfg)
	} else {
		port := "80"
		if request.URL.Port() != "" {
			port = request.URL.Port()
		}
		dial, err = net.Dial("tcp", fmt.Sprintf("%s:%s", request.URL.Hostname(), port))
	}
	if err != nil {
		return fmt.Errorf("ошибка подключения к хосту: %s", err)
	}
	response, err := p.SendRequest(dial, request)
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

func (p Proxy) SendRequest(dial net.Conn, req *http.Request) (*http.Response, error) {
	// чтобы тело можно было читать повторно в других местах
	if req.Body != nil {
		buf, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewBuffer(buf))
	}

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
