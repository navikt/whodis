package httpsupport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

var client = http.Client{}

func MakeGetRequest[T any](uri string) (*T, error) {
	resp, err := http.Get(uri)
	if err != nil {
		return new(T), err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return new(T), err
	}
	response := new(T)
	err = json.Unmarshal(body, &response)
	if err != nil {
		return new(T), err
	}
	return response, nil
}

func MakePostRequest(uri string, authToken string, reqBody []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", uri, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"Content-Type":  []string{"application/json"},
		"Authorization": []string{"Bearer " + authToken},
		"User-Agent":    {"Your friendly Nav Bot"},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	resBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return resBody, nil
}

func MakeGqlRequest[T any](uri string, authToken string, reqBody []byte) (*T, error) {
	resBody, err := MakePostRequest(uri, authToken, reqBody)
	if err != nil {
		return new(T), err
	}
	if isError(resBody) {
		return new(T), fmt.Errorf("error making GraphQL request: %s", resBody)
	}
	var deserialized T
	if err = json.Unmarshal(resBody, &deserialized); err != nil {
		return new(T), err
	}
	return &deserialized, nil
}

type ErrorResponse struct {
	Message string
	Status  string
}

func isError(responseBody []byte) bool {
	var rawResponse ErrorResponse
	if err := json.Unmarshal(responseBody, &rawResponse); err != nil {
		return true
	}
	return rawResponse.Status != "" && rawResponse.Status != "200"
}
