package secretmessage

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/lithammer/shortuuid"
	"github.com/prometheus/common/log"
	"github.com/slack-go/slack"

	"github.com/stretchr/testify/assert"
)

func postRequest(r http.Handler, body io.Reader, headers map[string]string, method, path string) *httptest.ResponseRecorder {

	req, _ := http.NewRequest(method, path, body)
	for h, v := range headers {
		req.Header.Add(h, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHandleSlash(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	defer s.Close()

	requestBody := url.Values{
		"command":         []string{"/secret"},
		"team_domain":     []string{"myteam"},
		"enterprise_id":   []string{"E0001"},
		"enterprise_name": []string{"Globular%20Construct%20Inc"},
		"channel_id":      []string{"C1234ABCD"},
		"text":            []string{"this is my secret"},
		"team_id":         []string{"T1234ABCD"},
		"user_id":         []string{"U1234ABCD"},
		"user_name":       []string{"imafish"},
		"response_url":    []string{"https://hooks.slack.com/commands/XXXXXXXX/00000000000/YYYYYYYYYYYYYY"},
		"token":           []string{"xoxb-1234"},
		"channel_name":    []string{"fishbowl"},
		"trigger_id":      []string{"0000000000.1111111111.222222222222aaaaaaaaaaaaaa"},
	}
	// Grab our router
	config := Config{
		RedisAddress:            s.Addr(),
		SigningSecret:           "secret",
		SkipSignatureValidation: true,
	}
	router := SetupRouter(config)
	// Perform a GET request with that handler.

	w := postRequest(router, strings.NewReader(requestBody.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, "POST", "/slash")

	assert.Equal(t, http.StatusOK, w.Code)

	var response slack.Message
	b, err := ioutil.ReadAll(w.Body)
	err = json.Unmarshal(b, &response)

	assert.Nil(t, err)

	if len(response.Attachments) < 1 {
		assert.FailNow(t, "Expected at least 1 response.Attachments")
	}
	assert.Equal(t, "imafish sent a secret message", response.Attachments[0].Title)
	if len(response.Attachments[0].Actions) < 1 {
		assert.FailNow(t, "Expected at least 1 response.Attachments[0].Actions")
	}
	assert.Equal(t, ":envelope: Read message", response.Attachments[0].Actions[0].Text)

	redisClient := GetRedisClient()
	secretID := strings.ReplaceAll(response.Attachments[0].CallbackID, "get_secret:", "")
	res, err := redisClient.Get(hash(secretID)).Result()
	decryptedSecret, err := decrypt(res, secretID)
	assert.Equal(t, "this is my secret", decryptedSecret)
	assert.Nil(t, err)
}
func TestHandleInteractiveGetSecret(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	defer s.Close()
	// Grab our router
	config := Config{
		RedisAddress:            s.Addr(),
		SigningSecret:           "secret",
		SkipSignatureValidation: true,
	}
	router := SetupRouter(config)

	secretID := shortuuid.New()
	redisClient := GetRedisClient()
	secretEncrypted, err := encrypt("this is my secret", secretID)
	if err != nil {
		assert.Fail(t, err.Error())
	}
	redisClient.Set(hash(secretID), secretEncrypted, 0)

	interactionPayload := slack.InteractionCallback{
		CallbackID: fmt.Sprintf("get_secret:%v", secretID),
	}

	interactionBytes, err := json.Marshal(interactionPayload)
	if err != nil {
		assert.Fail(t, err.Error())
	}

	requestBody := url.Values{
		"payload": []string{string(interactionBytes)},
	}
	w := postRequest(router, strings.NewReader(requestBody.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, "POST", "/interactive")
	assert.Equal(t, http.StatusOK, w.Code)

	var response slack.Message
	b, err := ioutil.ReadAll(w.Body)
	err = json.Unmarshal(b, &response)

	assert.Nil(t, err)

	log.Infof("%+v", response)
	if len(response.Attachments) < 1 {
		assert.FailNow(t, "Expected at least 1 response.Attachments")
	}
	assert.Equal(t, "Secret message", response.Attachments[0].Title)
	if len(response.Attachments[0].Actions) < 1 {
		assert.FailNow(t, "Expected at least 1 response.Attachments[0].Actions")
	}
	assert.Equal(t, "this is my secret", response.Attachments[0].Text)
	assert.Equal(t, ":x: Delete message", response.Attachments[0].Actions[0].Text)

}
func TestHandleInteractiveDeleteSecret(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	defer s.Close()
	// Grab our router
	config := Config{
		RedisAddress:            s.Addr(),
		SigningSecret:           "secret",
		SkipSignatureValidation: true,
	}
	router := SetupRouter(config)

	secretID := shortuuid.New()
	redisClient := GetRedisClient()
	secretEncrypted, err := encrypt("this is my secret", secretID)
	if err != nil {
		assert.Fail(t, err.Error())
	}
	redisClient.Set(hash(secretID), secretEncrypted, 0)

	interactionPayload := slack.InteractionCallback{
		CallbackID: fmt.Sprintf("delete_secret:%v", secretID),
	}

	interactionBytes, err := json.Marshal(interactionPayload)
	if err != nil {
		assert.Fail(t, err.Error())
	}

	requestBody := url.Values{
		"payload": []string{string(interactionBytes)},
	}
	w := postRequest(router, strings.NewReader(requestBody.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, "POST", "/interactive")
	assert.Equal(t, http.StatusOK, w.Code)

	var response slack.Message
	b, err := ioutil.ReadAll(w.Body)
	err = json.Unmarshal(b, &response)
	assert.Nil(t, err)
	log.Infof("%+v", response)
	assert.Equal(t, true, response.DeleteOriginal)
}