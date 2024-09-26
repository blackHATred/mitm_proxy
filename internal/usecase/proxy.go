package usecase

import (
	"crypto/tls"
	"net"
	"net/http"
)

type ProxyUsecase interface {
	HandleConn(conn net.Conn) error
	GetTLSConfig(host string) (*tls.Config, error)
	HandleHTTPSConnect(conn net.Conn, req *http.Request) error
	HandleHTTPRequest(conn net.Conn, request *http.Request, port int) error
	SendRequest(dial net.Conn, req *http.Request) (*http.Response, error)
}
