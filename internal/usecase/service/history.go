package service

import (
	"bufio"
	"crypto/tls"
	"github.com/blackHATred/mitm_proxy/internal/entity"
	"github.com/blackHATred/mitm_proxy/internal/repository"
	"github.com/blackHATred/mitm_proxy/internal/usecase"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
)

type History struct {
	HistoryRepository repository.History
	params            []string
}

func NewHistoryUsecase(historyRepo repository.History, filename string) (usecase.HistoryUsecase, error) {
	h := &History{
		HistoryRepository: historyRepo,
		params:            make([]string, 0),
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		h.params = append(h.params, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return h, nil
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
			// отключаем следование переадресации
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			// отключаем проверку сертификатов
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

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
	// Реализуем атаку param miner, параметры берем из params.txt со случайным значением.
	// Если в ответе есть параметр, который указан в params.txt, то добавляем его в ParamMinerObject
	obj, err := h.HistoryRepository.GetHistoryObject(id)
	if err != nil {
		return nil, err
	}

	req, err := entity.DeserializeRequest(obj.Request)
	if err != nil {
		return nil, err
	}

	paramMinerObject := &entity.ParamMinerObject{
		Param: make(map[string]entity.SerializablePair),
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	for _, param := range h.params {
		log.Printf("url: %s param: %s", req.URL, param)

		clonedReq := req.Clone(req.Context())
		q := clonedReq.URL.Query()
		randomValue := randomString(10)
		q.Set(param, randomValue)
		clonedReq.URL.RawQuery = q.Encode()

		res, err := client.Do(clonedReq)
		if err != nil {
			return nil, err
		}

		bodyBytes, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		res.Body.Close()

		bodyString := string(bodyBytes)
		if !strings.Contains(bodyString, randomValue) {
			continue // игнорируем, если изменений нет
		}

		res.Body = io.NopCloser(strings.NewReader(bodyString))

		serializedReq, err := entity.SerializeRequest(clonedReq)
		if err != nil {
			return nil, err
		}
		serializedRes, err := entity.SerializeResponse(res)
		if err != nil {
			return nil, err
		}

		paramMinerObject.Param[param] = entity.SerializablePair{
			Request:  *serializedReq,
			Response: *serializedRes,
		}
	}

	return paramMinerObject, nil
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
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
