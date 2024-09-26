package delivery

import (
	"context"
	"errors"
	"github.com/blackHATred/mitm_proxy/internal/usecase"
	"log"
	"net"
	"sync"
)

type Proxy struct {
	listener       net.Listener
	wg             sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
	historyUsecase usecase.HistoryUsecase
	proxyUsecase   usecase.ProxyUsecase
}

func NewProxy(h usecase.HistoryUsecase, p usecase.ProxyUsecase) *Proxy {
	proxy := &Proxy{}
	proxy.ctx, proxy.cancel = context.WithCancel(context.Background())
	proxy.historyUsecase = h
	proxy.wg = sync.WaitGroup{}
	proxy.proxyUsecase = p
	return proxy
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
			err := p.proxyUsecase.HandleConn(conn)
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
