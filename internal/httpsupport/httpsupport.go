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

func MakeUnauthenticatedGetRequest(uri string) ([]byte, error) {
	resp, err := http.Get(uri)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET request to %s failed with status %d", uri, resp.StatusCode)
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
		return nil, fmt.Errorf("GET request to %s failed with status %d", uri, resp.StatusCode)
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
		resBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("expected a 200-series status from %s, got %d (%s)", uri, resp.StatusCode, resBody)
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
	Errors []struct{} `json:"errors"`
}

func isError(responseBody []byte) bool {
	var rawResponse ErrorResponse
	if err := json.Unmarshal(responseBody, &rawResponse); err != nil {
		return true
	}
	return len(rawResponse.Errors) > 0
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
