package repository

import (
	"crypto/tls"
	"github.com/blackHATred/mitm_proxy/internal/entity"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
)

type History interface {
	GenerateCertificate(host string) (*tls.Certificate, error)
	GetCertificate(host string) (*tls.Certificate, error)
	AddHistory(req *http.Request, res *http.Response) (primitive.ObjectID, error)
	GetHistoryObject(id string) (*entity.HistoryObject, error)
	GetAllHistory() ([]entity.RequestListElem, error)
}
