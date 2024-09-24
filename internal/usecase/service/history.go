package service

import (
	"crypto/tls"
	"github.com/blackHATred/mitm_proxy/internal/entity"
	"github.com/blackHATred/mitm_proxy/internal/repository"
	"github.com/blackHATred/mitm_proxy/internal/usecase"
	"net/http"
)

type History struct {
	HistoryRepository repository.History
}

func NewHistoryUsecase(historyRepo repository.History) usecase.HistoryUsecase {
	return &History{
		HistoryRepository: historyRepo,
	}
}

func (h *History) RequestRepeat(id string) (string, error) {
	obj, err := h.HistoryRepository.GetHistoryObject(id)
	if err != nil {
		return "", err
	}

	req, err := entity.DeserializeRequest(obj.Request)
	if err != nil {
		return "", err
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Отключаем следование переадресации
			return http.ErrUseLastResponse
		},
	}

	// Отправляем запрос и получаем ответ
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	newID, err := h.HistoryRepository.AddHistory(req, res)
	if err != nil {
		return "", err
	}
	return newID.Hex(), nil
}

func (h *History) RequestDetails(id string) (*entity.HistoryObject, error) {
	obj, err := h.HistoryRepository.GetHistoryObject(id)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

func (h *History) RequestScan(id string) (*entity.ParamMinerObject, error) {
	//TODO implement me
	panic("implement me")
}

func (h *History) RequestList() ([]entity.RequestListElem, error) {
	list, err := h.HistoryRepository.GetAllHistory()
	if err != nil {
		return nil, err
	}

	return list, nil
}

func (h *History) AddHistory(req *http.Request, res *http.Response) error {
	_, err := h.HistoryRepository.AddHistory(req, res)
	return err
}

func (h *History) GetCertificate(host string) (*tls.Certificate, error) {
	return h.HistoryRepository.GetCertificate(host)
}
