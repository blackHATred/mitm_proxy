package internal

import (
	"context"
	"fmt"
	"github.com/blackHATred/mitm_proxy/internal/entity"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"html/template"
	"log"
	"net/http"
	"strings"
)

type HistoryService struct {
	db *mongo.Database
}

func (h *HistoryService) webRequests(w http.ResponseWriter, r *http.Request) {
	cursor, err := h.db.Collection("history").Find(context.TODO(), bson.M{})
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка чтения из базы данных: %s", err), http.StatusInternalServerError)
		return
	}

	var history []bson.M
	err = cursor.All(context.TODO(), &history)
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка сериализации истории запросов: %s", err), http.StatusInternalServerError)
		return
	}

	tmpl, err := template.ParseFiles("templates/requests.html")
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка загрузки шаблона: %s", err), http.StatusInternalServerError)
		return
	}

	data := make([]map[string]string, len(history))
	for i, h := range history {
		data[i] = map[string]string{
			"ID":       h["_id"].(primitive.ObjectID).Hex(),
			"DateTime": h["datetime"].(string),
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка выполнения шаблона: %s", err), http.StatusInternalServerError)
	}
}

func (h *HistoryService) requestDetails(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/requests/")
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	var historyObject entity.HistoryObject
	err = h.db.Collection("history").FindOne(context.TODO(), bson.M{"_id": objID}).Decode(&historyObject)
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка чтения из базы данных: %s", err), http.StatusInternalServerError)
		return
	}

	// Добавляем поле ID в данные для шаблона
	data := struct {
		entity.HistoryObject
		ID string
	}{
		HistoryObject: historyObject,
		ID:            id,
	}

	tmpl, err := template.ParseFiles("templates/request_details.html")
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка загрузки шаблона: %s", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка выполнения шаблона: %s", err), http.StatusInternalServerError)
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

func (h *HistoryService) repeatRequest(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/repeat/")
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	var historyObject entity.HistoryObject
	err = h.db.Collection("history").FindOne(context.TODO(), bson.M{"_id": objID}).Decode(&historyObject)
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка чтения из базы данных: %s", err), http.StatusInternalServerError)
		return
	}

	// Восстанавливаем запрос из сохраненной истории
	req, err := deserializeRequest(historyObject.Request)
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка десериализации запроса: %s", err), http.StatusInternalServerError)
		return
	}

	// Настраиваем кастомный HTTP клиент
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Отключаем следование переадресации
			return http.ErrUseLastResponse
		},
	}

	// Отправляем запрос и получаем ответ
	res, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка отправки запроса: %s", err), http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	// Сохраняем новый запрос и ответ в истории
	newID, err := h.AddHistory(req, res)
	if err != nil {
		http.Error(w, fmt.Sprintf("ошибка сохранения истории: %s", err), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/requests/%s", newID.Hex()), http.StatusSeeOther)
}

func (h *HistoryService) ListenAndServe() error {
	// web интерфейс для api
	mux := http.NewServeMux()
	mux.HandleFunc("/requests", h.webRequests)
	mux.HandleFunc("/requests/", h.requestDetails)
	mux.HandleFunc("/repeat/", h.repeatRequest)
	log.Println("Запуск веб-сервера на порту 8000")
	return http.ListenAndServe(":8000", mux)
}

func (h *HistoryService) Close() {
	err := h.db.Client().Disconnect(context.TODO())
	if err != nil {
		log.Fatalf("Ошибка при отключении от MongoDB: %s", err)
	}
}
