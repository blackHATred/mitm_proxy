package usecase

import (
	"crypto/tls"
	"github.com/blackHATred/mitm_proxy/internal/entity"
	"net/http"
)

type HistoryUsecase interface {
	RequestRepeat(id string) (string, error)
	RequestDetails(id string) (*entity.HistoryObject, error)
	RequestScan(id string) (*entity.ParamMinerObject, error)
	RequestList() ([]entity.RequestListElem, error)
	AddHistory(req *http.Request, res *http.Response) error
	GetCertificate(host string) (*tls.Certificate, error)
}
