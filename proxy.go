package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// Proxy - структура, которая реализует интерфейс http.Handler
type Proxy struct {
	client *http.Client
}

func logRequest(prefix string, request *http.Request) {
	stringBuilder := new(strings.Builder)
	stringBuilder.WriteString("\n" + prefix + "\n")
	stringBuilder.WriteString(fmt.Sprintf("%s %s %s\n", request.Method, request.RequestURI, request.Proto))
	stringBuilder.WriteString(fmt.Sprintf("Host: %s\n", request.Host))
	for key, values := range request.Header {
		for _, value := range values {
			stringBuilder.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		}
	}
	log.Print(stringBuilder.String(), "\n")
}

func logResponse(prefix string, response *http.Response) {
	stringBuilder := new(strings.Builder)
	stringBuilder.WriteString(prefix + "\n")
	stringBuilder.WriteString(fmt.Sprintf("%s %s\n", response.Proto, response.Status))
	for key, values := range response.Header {
		for _, value := range values {
			stringBuilder.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		}
	}
	log.Print(stringBuilder.String(), "\n")
}

func (p *Proxy) SetClient() {
	p.client = http.DefaultClient
	// отключаем редирект по умолчанию
	p.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// сделаем свой собственный заголовок специально для прокси, который позволит следовать редиректу
		if req.Header.Get("Proxy-Redirect") != "" {
			return nil
		}
		return http.ErrUseLastResponse
	}
}

func (p *Proxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	logRequest("Входящий запрос:", request)
	fmt.Println("URL:", request.URL.String())
	newRequest, err := http.NewRequest(request.Method, request.URL.String(), request.Body)
	if err != nil {
		log.Fatalf("Ошибка создания запроса для проксирования: %s", err)
		return
	}
	newRequest.Header = request.Header.Clone()
	newRequest.Header.Del("Proxy-Connection")
	logRequest("Изменённый запрос:", newRequest)

	// если безопасное подключение
	if request.Method == http.MethodConnect {

	}

	// отправляем запрос
	response, err := p.client.Do(newRequest)
	if err != nil {
		log.Fatalf("Ошибка отправки запроса: %s", err)
		return
	}
	logResponse("Ответ:", response)

	// читаем ответ и пишем его в writer

	// копируем response status
	writer.WriteHeader(response.StatusCode)

	// пишем все заголовки
	for key, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}
	// пишем тело ответа
	defer response.Body.Close()
	_, err = io.Copy(writer, response.Body)
	if err != nil {
		log.Fatalf("Ошибка копирования тела ответа: %s", err)
		return
	}
}
