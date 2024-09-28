package mongo

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/blackHATred/mitm_proxy/internal/entity"
	"github.com/blackHATred/mitm_proxy/internal/repository"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"math/big"
	"net"
	"net/http"
	"os"
	"time"
)

type historyDB struct {
	db     *mongo.Database
	ctx    context.Context
	caKey  *tls.Certificate
	caCert *x509.Certificate
}

func NewHistoryRepository(db *mongo.Database, caKeyFilename, caCertFilename string) (repository.History, error) {
	// Загрузка приватного ключа CA
	caKeyPEM, err := os.ReadFile(caKeyFilename)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ключа CA: %s", err)
	}
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	caKey, err := x509.ParsePKCS8PrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга ключа CA: %s", err)
	}

	// Загрузка сертификата CA
	caCertPEM, err := os.ReadFile(caCertFilename)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения сертификата CA: %s", err)
	}
	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil || caCertBlock.Type != "CERTIFICATE" {
		return nil, errors.New("некорректный PEM блок для сертификата CA")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга сертификата CA: %s", err)
	}

	return &historyDB{
		db:  db,
		ctx: context.Background(),
		caKey: &tls.Certificate{
			Certificate: [][]byte{caCert.Raw},
			PrivateKey:  caKey,
		},
		caCert: caCert,
	}, nil
}

func (h *historyDB) GenerateCertificate(host string) (*tls.Certificate, error) {
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

	// Определение параметров временного сертификата
	certTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   host,
			Organization: []string{"Organization"},
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
			certTemplate.IPAddresses = append(certTemplate.IPAddresses, ip)
		} else {
			certTemplate.DNSNames = append(certTemplate.DNSNames, h)
		}
	}

	// Подписываем временный сертификат корневым сертификатом (CA)
	certBytes, err := x509.CreateCertificate(rand.Reader, &certTemplate, h.caCert, &priv.PublicKey, h.caKey.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания сертификата: %s", err)
	}

	// Приватный ключ ECDSA в PEM-формате
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("ошибка маршалинга приватного ключа: %s", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privBytes,
	})

	// Сертификат в PEM-формате
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

func (h *historyDB) GetCertificate(host string) (*tls.Certificate, error) {
	// поиск сертификата в базе данных
	var certData bson.M
	err := h.db.Collection("certificates").FindOne(h.ctx, bson.M{"host": host}).Decode(&certData)
	if errors.Is(err, mongo.ErrNoDocuments) {
		// если сертификат не найден, генерируем новый
		cert, err := h.GenerateCertificate(host)
		if err != nil {
			return nil, fmt.Errorf("ошибка генерации сертификата: %s", err)
		}

		// сериализуем сертификат и ключ в PEM-формат
		certPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert.Certificate[0],
		})

		// приватный ключ должен быть маршализован правильно для ECDSA
		privKeyBytes, err := x509.MarshalECPrivateKey(cert.PrivateKey.(*ecdsa.PrivateKey))
		if err != nil {
			return nil, fmt.Errorf("ошибка маршалинга EC приватного ключа: %s", err)
		}

		keyPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: privKeyBytes,
		})

		// сохраняем сертификат и ключ в базу данных
		_, err = h.db.Collection("certificates").InsertOne(h.ctx, bson.M{
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

	// десериализуем сертификат и ключ из PEM-формата
	certPEM := certData["certPEM"].(string)
	keyPEM := certData["keyPEM"].(string)

	// парсим ключ и сертификат
	tlsCert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки X509KeyPair из базы данных: %s", err)
	}

	return &tlsCert, nil
}

func (h *historyDB) AddHistory(req *http.Request, res *http.Response) (primitive.ObjectID, error) {
	serializedReq, err := entity.SerializeRequest(req)
	if err != nil {
		return primitive.NilObjectID, err
	}
	serializedRes, err := entity.SerializeResponse(res)
	if err != nil {
		return primitive.NilObjectID, err
	}
	historyObject := entity.HistoryObject{
		Request:  *serializedReq,
		Response: *serializedRes,
		DateTime: time.Now().Format(time.RFC3339),
	}
	result, err := h.db.Collection("history").InsertOne(h.ctx, historyObject)
	if err != nil {
		return primitive.NilObjectID, fmt.Errorf("ошибка записи в базу данных: %s", err)
	}
	return result.InsertedID.(primitive.ObjectID), nil
}

func (h *historyDB) GetHistoryObject(id string) (*entity.HistoryObject, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}

	var historyObject entity.HistoryObject
	err = h.db.Collection("history").FindOne(h.ctx, bson.M{"_id": objID}).Decode(&historyObject)
	if err != nil {
		return nil, err
	}

	return &historyObject, err
}

func (h *historyDB) GetAllHistory() ([]entity.RequestListElem, error) {
	cursor, err := h.db.Collection("history").Find(h.ctx, bson.M{})
	if err != nil {
		return nil, err
	}

	var historyBSON []bson.M
	err = cursor.All(h.ctx, &historyBSON)
	if err != nil {
		return nil, err
	}

	data := make([]entity.RequestListElem, len(historyBSON))
	for i, elem := range historyBSON {
		data[i] = entity.RequestListElem{
			ID:       elem["_id"].(primitive.ObjectID).Hex(),
			DateTime: elem["datetime"].(string),
		}
	}

	return data, nil
}
