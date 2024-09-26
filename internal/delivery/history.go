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

	d.templates = make(map[string]*template.Template)
	tmpl, err := template.ParseFiles("templates/requests.html")
	if err != nil {
		return nil, err
	}
	d.templates["requests"] = tmpl
	tmpl, err = template.ParseFiles("templates/request_details.html")
	if err != nil {
		return nil, err
	}
	d.templates["request_details"] = tmpl
	tmpl, err = template.ParseFiles("templates/scanned_params.html")
	if err != nil {
		return nil, err
	}
	d.templates["scanned_params"] = tmpl

	return &d, nil
}

func (h *History) StartHttpServer(wg *sync.WaitGroup, mux *http.ServeMux, addr string) *http.Server {
	srv := &http.Server{Addr: addr}
	mux.HandleFunc("/requests", h.RequestsList)
	mux.HandleFunc("/requests/", h.RequestDetails)
	mux.HandleFunc("/repeat/", h.RequestRepeat)
	mux.HandleFunc("/scan/", h.Scan)
	mux.HandleFunc("/", h.Example)
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

func (h *History) Scan(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/scan/")
	_, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		http.Error(w, "Невалидный формат ID", http.StatusBadRequest)
		return
	}

	// включаем режим chunked передачи данных
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Transfer-Encoding", "chunked")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Chunked Transfer Encoding не поддерживается", http.StatusInternalServerError)
		return
	}

	_, err = w.Write([]byte("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n    <meta charset=\"UTF-8\">\n    <title>Scanned Params</title>\n    <link href=\"https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/css/bootstrap.min.css\" rel=\"stylesheet\" integrity=\"sha384-QWTKZyjpPEjISv5WaRU9OFeRpok6YctnYmDr5pNlyT2bRjXh0JMhjY6hW+ALEwIH\" crossorigin=\"anonymous\">\n</head>\n<body>\n<h1>Сканирование параметров...</h1>\n<h3>Не закрывайте и не перезагружайте страницу, пока не увидите надпись \"Сканирование завершено\". </h3>\n<h3>В зависимости от количества параметров в params.txt это может занять некоторое время.</h3>"))
	if err != nil {
		http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %v", err), http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	searchedParams, err := h.historyUsecase.RequestScan(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %v", err), http.StatusInternalServerError)
		return
	}

	err = h.templates["scanned_params"].Execute(w, searchedParams)
	if err != nil {
		http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %s", err), http.StatusInternalServerError)
	}

	_, err = w.Write([]byte("<h1>Сканирование завершено</h1>\n</body>\n</html>"))
	if err != nil {
		http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %v", err), http.StatusInternalServerError)
		return
	}
	flusher.Flush()
}

func (h *History) Example(w http.ResponseWriter, r *http.Request) {
	// если в запросе есть параметр "url", то возвращаем его в теле ответа, иначе возвращаем "Hello, World!"
	url := r.URL.Query().Get("url")
	if url != "" {
		_, err := w.Write([]byte(url))
		if err != nil {
			http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %v", err), http.StatusInternalServerError)
		}
		return
	}

	_, err := w.Write([]byte("Hello, World!"))
	if err != nil {
		http.Error(w, fmt.Sprintf("Произошла внутренняя ошибка сервера: %v", err), http.StatusInternalServerError)
	}
}
