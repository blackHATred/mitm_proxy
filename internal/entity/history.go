package entity

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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

type SerializablePair struct {
	Request  SerializableRequest  `bson:"request"`
	Response SerializableResponse `bson:"response"`
}

type ParamMinerObject struct {
	Param map[string]SerializablePair `bson:"param"`
}

type RequestListElem struct {
	ID       string `template:"ID"`
	DateTime string `template:"DateTime"`
}

func SerializeRequest(req *http.Request) (*SerializableRequest, error) {
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	header := make(map[string]string)
	for k, v := range req.Header {
		header[k] = strings.Join(v, ", ")
	}

	var url string
	if req.URL.String() == "/" {
		// https запрос
		url = fmt.Sprintf("https://%s%s", req.Host, req.URL.String())
	} else {
		url = req.URL.String()
	}

	return &SerializableRequest{
		Method: req.Method,
		URL:    url,
		Header: header,
		Body:   string(bodyBytes),
	}, nil
}

func DeserializeRequest(serializedReq SerializableRequest) (*http.Request, error) {
	// Создаем новый запрос
	req, err := http.NewRequest(serializedReq.Method, serializedReq.URL, bytes.NewBufferString(serializedReq.Body))
	if err != nil {
		return nil, err
	}

	// Восстанавливаем заголовки
	for k, v := range serializedReq.Header {
		req.Header.Set(k, v)
	}

	return req, nil
}

func SerializeResponse(res *http.Response) (*SerializableResponse, error) {
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	res.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Reset the body for further use

	header := make(map[string]string)
	for k, v := range res.Header {
		header[k] = strings.Join(v, ", ")
	}

	return &SerializableResponse{
		StatusCode: res.StatusCode,
		Header:     header,
		Body:       string(bodyBytes),
	}, nil
}
