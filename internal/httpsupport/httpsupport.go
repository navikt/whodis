package httpsupport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var client = http.Client{}

type HttpError struct {
	Code int
}

func (e *HttpError) Error() string {
	return fmt.Sprintf("HTTP request failed: %d", e.Code)
}

func MakeUnauthenticatedGetRequest(uri string) ([]byte, error) {
	resp, err := http.Get(uri)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &HttpError{resp.StatusCode}
	}
	return readResponse(resp)
}

func MakeAuthenticatedGetRequest(uri, authToken string) ([]byte, error) {
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"Authorization": []string{"Bearer " + authToken},
		"User-Agent":    {"Your friendly Nav Bot"},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &HttpError{resp.StatusCode}
	}
	return readResponse(resp)
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
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, &HttpError{resp.StatusCode}
	}
	resBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return resBody, nil
}

func MakeGqlQuery[T any](uri string, authToken string, query string) (*T, error) {
	queryAsSingleLine := strings.Replace(query, "\n", " ", -1)
	reqBody := `{ "query": " ` + queryAsSingleLine + ` " }`
	resBody, err := MakePostRequest(uri, authToken, []byte(reqBody))
	if err != nil {
		return new(T), err
	}
	if gqlErr := gqlError(resBody); gqlErr != nil {
		return new(T), gqlErr
	}
	var deserialized T
	if err = json.Unmarshal(resBody, &deserialized); err != nil {
		return new(T), err
	}
	return &deserialized, nil
}

type GqlError struct {
	Message string
}

func (e *GqlError) Error() string {
	return e.Message
}

type errorResponse struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func gqlError(responseBody []byte) error {
	var raw errorResponse
	if err := json.Unmarshal(responseBody, &raw); err != nil {
		return &GqlError{Message: "failed to parse GraphQL response"}
	}
	if len(raw.Errors) > 0 {
		return &GqlError{Message: raw.Errors[0].Message}
	}
	return nil
}

func readResponse(resp *http.Response) ([]byte, error) {
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("expected 200 from %s, got %s, ", resp.Request.URL.String(), resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}
