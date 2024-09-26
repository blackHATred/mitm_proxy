package main

import (
	"context"
	"flag"
	"github.com/blackHATred/mitm_proxy/internal/delivery"
	mongoRepo "github.com/blackHATred/mitm_proxy/internal/repository/mongo"
	"github.com/blackHATred/mitm_proxy/internal/usecase/service"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	var proxyURI = flag.String("proxy", ":8000", "Ссылка для подключения к прокси")
	var mongoURI = flag.String("db", "mongodb://localhost:27017", "Ссылка для подключения к Mongo")
	var webAddr = flag.String("addr", ":8080", "Адрес web-интерфейса")
	flag.Parse()

	wg := &sync.WaitGroup{}
	wg.Add(2)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	clientOptions := options.Client().ApplyURI(*mongoURI)
	db, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		log.Fatalf("Ошибка подключения к MongoDB: %v", err)
	}
	err = db.Ping(context.Background(), nil)
	if err != nil {
		log.Fatalf("Не удалось подключиться к MongoDB: %v", err)
	}

	historyRepo := mongoRepo.NewHistoryRepository(db.Database("proxyDB"))
	historyUC, err := service.NewHistoryUsecase(historyRepo, "resources/params.txt")
	if err != nil {
		log.Fatalf("Произошла ошибка при инициализации: %v", err)
	}
	historyDelivery, err := delivery.NewHistoryDelivery(historyUC)
	if err != nil {
		log.Fatalf("Произошла ошибка при инициализации: %v", err)
	}
	proxyUsecase := service.NewProxyService(historyUC)
	proxyDelivery := delivery.NewProxy(historyUC, proxyUsecase)
	err = proxyDelivery.StartProxyServer(wg, *proxyURI)
	if err != nil {
		log.Fatalf("Произошла ошибка при запуске прокси-сервера: %v", err)
	}
	historyServer := historyDelivery.StartHttpServer(wg, http.NewServeMux(), *webAddr)

	// ждем сигнала от системы об завершении работы
	<-sig
	log.Printf("Получен сигнал завершения работы, выполняем graceful shutdown")
	// отключаем веб-сервер
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := historyServer.Shutdown(ctx); err != nil {
			log.Fatalf("Не удалось выполнить graceful shutdown для веб-сервера: %v", err)
		}
		log.Printf("Веб-сервер остановлен")
		cancel()
		wg.Done()
	}()
	// отключаем прокси
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := proxyDelivery.Shutdown(ctx); err != nil {
			log.Fatalf("Не удалось выполнить graceful shutdown для прокси-сервера: %v", err)
		}
		log.Printf("Прокси-сервер остановлен")
		cancel()
		wg.Done()
	}()
	wg.Wait()
}
