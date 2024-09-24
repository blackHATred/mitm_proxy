package delivery

import (
	"errors"
	"fmt"
	"github.com/blackHATred/mitm_proxy/internal/entity"
	"github.com/blackHATred/mitm_proxy/internal/usecase"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"
)

type History struct {
	historyUsecase usecase.HistoryUsecase
	templates      map[string]*template.Template
}

func NewHistoryDelivery(historyUC usecase.HistoryUsecase) (*History, error) {
	d := History{
		historyUsecase: historyUC,
	}

	tmpl, err := template.ParseFiles("templates/requests.html")
	if err != nil {
		return nil, err
	}
	d.templates = make(map[string]*template.Template)
	d.templates["requests"] = tmpl
	tmpl, err = template.ParseFiles("templates/request_details.html")
	if err != nil {
		return nil, err
	}
	d.templates["request_details"] = tmpl

	return &d, nil
}

func (h *History) StartHttpServer(wg *sync.WaitGroup, mux *http.ServeMux, addr string) *http.Server {
	srv := &http.Server{Addr: addr}
	mux.HandleFunc("/requests", h.RequestsList)
	mux.HandleFunc("/requests/", h.RequestDetails)
	mux.HandleFunc("/repeat/", h.RequestRepeat)
	srv.Handler = mux

	go func() {
		defer wg.Done()

		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("ListenAndServe(): %v", err)
		}
	}()

	log.Printf("HTTP-сервер запущен на %s", addr)
	return srv
}

func (h *History) RequestsList(w http.ResponseWriter, r *http.Request) {
	list, err := h.historyUsecase.RequestList()
	if err != nil {
		http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %s", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = h.templates["requests"].Execute(w, list)
	if err != nil {
		http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %s", err), http.StatusInternalServerError)
	}
}

func (h *History) RequestDetails(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/requests/")
	_, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		http.Error(w, "Невалидный формат ID", http.StatusBadRequest)
		return
	}
	details, err := h.historyUsecase.RequestDetails(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %s", err), http.StatusInternalServerError)
		return
	}

	// Добавляем поле ID в данные для шаблона
	data := struct {
		entity.HistoryObject
		ID string
	}{
		HistoryObject: *details,
		ID:            id,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = h.templates["request_details"].Execute(w, data)
	if err != nil {
		http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %s", err), http.StatusInternalServerError)
	}
}

func (h *History) RequestRepeat(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/repeat/")
	_, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		http.Error(w, "Невалидный формат ID", http.StatusBadRequest)
		return
	}

	// Повторяем запрос
	newID, err := h.historyUsecase.RequestRepeat(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %v", err), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/requests/%s", newID), http.StatusSeeOther)
}
