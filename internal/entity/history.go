package entity

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type SerializableRequest struct {
	Method        string         `bson:"method"`
	URL           string         `bson:"url"`
	Header        http.Header    `bson:"header"`
	Body          string         `bson:"body"`
	ContentLength int64          `bson:"content_length"`
	Host          string         `bson:"host"`
	Cookies       []*http.Cookie `bson:"cookies"`
	PostForm      url.Values     `bson:"post_form"`
	Form          url.Values     `bson:"form"` // Содержит и URL-параметры, и POST-параметры
	Timestamp     time.Time      `bson:"timestamp"`
}

type SerializableResponse struct {
	Status        string         `bson:"status"`
	StatusCode    int            `bson:"status_code"`
	Header        http.Header    `bson:"header"`
	Body          string         `bson:"body"`
	ContentLength int64          `bson:"content_length"`
	Cookies       []*http.Cookie `bson:"cookies"`
	Timestamp     time.Time      `bson:"timestamp"`
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
	var u string
	if req.URL.Hostname() == "" {
		// https запрос
		u = fmt.Sprintf("https://%s%s", req.Host, req.URL.String())
	} else {
		u = req.URL.String()
	}

	var body string
	if req.Body != nil {
		buf, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		body = string(buf)
		req.Body = io.NopCloser(bytes.NewBuffer(buf))
	}

	err := req.ParseForm() // это парсит как query параметры, так и post параметры в формате application/x-www-form-urlencoded
	if err != nil {
		return nil, err
	}

	cookies := req.Cookies()

	requestData := SerializableRequest{
		Method:        req.Method,
		URL:           u,
		Header:        req.Header,
		Body:          body,
		ContentLength: req.ContentLength,
		Host:          req.Host,
		Cookies:       cookies,
		PostForm:      req.PostForm,
		Form:          req.Form,
		Timestamp:     time.Now(),
	}
	return &requestData, nil
}

func DeserializeRequest(serializedReq SerializableRequest) (*http.Request, error) {
	req, err := http.NewRequest(serializedReq.Method, serializedReq.URL, bytes.NewBuffer([]byte(serializedReq.Body)))
	if err != nil {
		return nil, err
	}

	req.Header = serializedReq.Header
	req.ContentLength = serializedReq.ContentLength
	req.Host = serializedReq.Host

	for _, cookie := range serializedReq.Cookies {
		req.AddCookie(cookie)
	}

	req.PostForm = serializedReq.PostForm
	req.Form = serializedReq.Form

	return req, nil
}

func SerializeResponse(res *http.Response) (*SerializableResponse, error) {
	var body string
	if res.Body != nil {
		buf, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		body = string(buf)
		res.Body = io.NopCloser(bytes.NewBuffer(buf))
	}

	cookies := res.Cookies()

	responseData := SerializableResponse{
		Status:        res.Status,
		StatusCode:    res.StatusCode,
		Header:        res.Header,
		Body:          body,
		ContentLength: res.ContentLength,
		Cookies:       cookies,
		Timestamp:     time.Now(),
	}

	return &responseData, nil
}
