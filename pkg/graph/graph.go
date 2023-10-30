package graph

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
)

type Graph struct {
	Client *http.Client
}

func (g *Graph) Run(payload map[string]interface{}, urlGraph string) ([]byte, error) {
	bytesRepresentation, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest(http.MethodPost, urlGraph, bytes.NewBuffer(bytesRepresentation))
	if err != nil {
		return nil, err
	}

	body, err := g.DoRequest(request)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (g *Graph) DoRequest(request *http.Request) ([]byte, error) {
	response, err := g.Client.Do(request)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			return
		}
	}(response.Body)

	return body, nil
}
