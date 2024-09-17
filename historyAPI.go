package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"
)

type HistoryService struct {
	db *mongo.Database
}

type SerializableRequest struct {
	Method string            `bson:"method"`
	URL    string            `bson:"url"`
	Header map[string]string `bson:"header"`
	Body   string            `bson:"body"`
}

type SerializableResponse struct {
	StatusCode int               `bson:"status_code"`
	Header     map[string]string `bson:"header"`
	Body       string            `bson:"body"`
}

type HistoryObject struct {
	Request  SerializableRequest  `bson:"request"`
	Response SerializableResponse `bson:"response"`
	DateTime string               `bson:"datetime"`
}

func serializeRequest(req *http.Request) (SerializableRequest, error) {
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return SerializableRequest{}, err
	}
	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Reset the body for further use

	header := make(map[string]string)
	for k, v := range req.Header {
		header[k] = strings.Join(v, ", ")
	}

	return SerializableRequest{
		Method: req.Method,
		URL:    req.URL.String(),
		Header: header,
		Body:   string(bodyBytes),
	}, nil
}

func serializeResponse(res *http.Response) (SerializableResponse, error) {
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return SerializableResponse{}, err
	}
	res.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Reset the body for further use

	header := make(map[string]string)
	for k, v := range res.Header {
		header[k] = strings.Join(v, ", ")
	}

	return SerializableResponse{
		StatusCode: res.StatusCode,
		Header:     header,
		Body:       string(bodyBytes),
	}, nil
}

func (h *HistoryService) webRequests(w http.ResponseWriter, r *http.Request) {
	// Получение истории запросов из базы данных
	cursor, err := h.db.Collection("history").Find(context.TODO(), bson.M{})
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка чтения из базы данных: %s", err), http.StatusInternalServerError)
		return
	}

	// Сериализация истории запросов в JSON
	var history []HistoryObject
	err = cursor.All(context.TODO(), &history)
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка сериализации истории запросов: %s", err), http.StatusInternalServerError)
		return
	}

	// Отправка истории запросов в виде JSON
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(history)
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка отправки ответа: %s", err), http.StatusInternalServerError)
		return
	}
}

func InitHistoryService() *HistoryService {
	// Инициализация MongoDB клиента
	clientOptions := options.Client().ApplyURI("mongodb://localhost:27017")
	client, err := mongo.Connect(context.TODO(), clientOptions)
	if err != nil {
		log.Fatalf("Ошибка подключения к MongoDB: %s", err)
	}
	err = client.Ping(context.TODO(), nil)
	if err != nil {
		log.Fatalf("Не удалось подключиться к MongoDB: %s", err)
	}

	return &HistoryService{db: client.Database("proxyDB")}
}

func (h *HistoryService) ListenAndServe() error {
	// web интерфейс для api
	mux := http.NewServeMux()
	mux.HandleFunc("/requests", h.webRequests)
	log.Println("Запуск веб-сервера на порту 8000")
	return http.ListenAndServe(":8000", mux)
}

func (h *HistoryService) Close() {
	err := h.db.Client().Disconnect(context.TODO())
	if err != nil {
		log.Fatalf("Ошибка при отключении от MongoDB: %s", err)
	}
}

func (h *HistoryService) GetCertificate(host string) (*tls.Certificate, error) {
	// Поиск сертификата в базе данных
	var certData bson.M
	err := h.db.Collection("certificates").FindOne(context.TODO(), bson.M{"host": host}).Decode(&certData)
	if errors.Is(err, mongo.ErrNoDocuments) {
		// Если сертификат не найден, генерируем новый
		cert, err := h.generateCertificate(host)
		if err != nil {
			return nil, fmt.Errorf("ошибка генерации сертификата: %s", err)
		}

		// Сериализуем сертификат и ключ в PEM-формат
		certPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert.Certificate[0],
		})

		// Приватный ключ должен быть маршализован правильно для ECDSA
		privKeyBytes, err := x509.MarshalECPrivateKey(cert.PrivateKey.(*ecdsa.PrivateKey))
		if err != nil {
			return nil, fmt.Errorf("ошибка маршалинга EC приватного ключа: %s", err)
		}

		keyPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: privKeyBytes,
		})

		// Сохраняем сертификат и ключ в базу данных
		_, err = h.db.Collection("certificates").InsertOne(context.TODO(), bson.M{
			"host":    host,
			"certPEM": string(certPEM),
			"keyPEM":  string(keyPEM),
		})
		if err != nil {
			return nil, fmt.Errorf("ошибка записи сертификата в базу данных: %s", err)
		}

		return cert, nil
	} else if err != nil {
		return nil, fmt.Errorf("ошибка поиска сертификата в базе данных: %s", err)
	}

	// Десериализуем сертификат и ключ из PEM-формата
	certPEM := certData["certPEM"].(string)
	keyPEM := certData["keyPEM"].(string)

	// Парсим ключ и сертификат
	tlsCert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки X509KeyPair из базы данных: %s", err)
	}

	return &tlsCert, nil
}

func (h *HistoryService) generateCertificate(host string) (*tls.Certificate, error) {
	// Генерация нового приватного ключа (ECDSA)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ошибка генерации ключа: %s", err)
	}

	// Создание серийного номера для сертификата
	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return nil, fmt.Errorf("ошибка генерации серийного номера: %s", err)
	}

	// Определение параметров сертификата
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   host,
			Organization: []string{"Крутой прокси с MITM"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // 1 год
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Добавляем IP-адреса и DNS-имена в сертификат
	hosts := []string{host}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	// Генерация самоподписанного сертификата
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания сертификата: %s", err)
	}

	ECPrivateKey, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("ошибка маршалинга EC-ключа: %s", err)
	}

	// переводим ключ в PEM-формат
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: ECPrivateKey,
	})

	// переводим сертификат в PEM-формат
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания X509KeyPair: %s", err)
	}

	return &tlsCert, nil
}

func (h *HistoryService) AddHistory(req *http.Request, res *http.Response) error {
	serializedReq, err := serializeRequest(req)
	if err != nil {
		return err
	}
	serializedRes, err := serializeResponse(res)
	if err != nil {
		return err
	}
	historyObject := HistoryObject{
		Request:  serializedReq,
		Response: serializedRes,
		DateTime: time.Now().Format(time.RFC3339),
	}
	_, err = h.db.Collection("history").InsertOne(context.TODO(), historyObject)
	if err != nil {
		log.Fatalf("Ошибка записи в базу данных: %s", err)
	}
	return nil
}
