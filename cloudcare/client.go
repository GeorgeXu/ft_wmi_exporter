package cloudcare

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"wmi_exporter/cfg"
	"wmi_exporter/git"

	"github.com/Go-zh/net/context/ctxhttp"
	"github.com/golang/protobuf/proto"
	"github.com/klauspost/compress/snappy"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"

	config_util "github.com/prometheus/common/config"
)

const maxErrMsgLen = 256

type Client struct {
	index   int // Used to differentiate clients in metrics.
	url     *config_util.URL
	client  *http.Client
	timeout time.Duration
}

// ClientConfig configures a Client.
type ClientConfig struct {
	URL              *config_util.URL
	Timeout          model.Duration
	HTTPClientConfig config_util.HTTPClientConfig
}

// NewClient creates a new Client.
func NewClient(index int, conf *ClientConfig) (*Client, error) {
	httpClient, err := config_util.NewClientFromConfig(conf.HTTPClientConfig, "remote_storage")
	if err != nil {
		return nil, err
	}

	return &Client{
		index:   index,
		url:     conf.URL,
		client:  httpClient,
		timeout: time.Duration(conf.Timeout),
	}, nil
}

type recoverableError struct {
	error
}

///  content  ---  上传的数据  body
///  contentType ---- 上传数据 格式  如“application/json”
///  dateStr  ----- header "Date"
///  key  ----- header "X-Prometheus-Key"
///  method ----  http request method ,eg PUT，GET，POST，HEAD，DELETE
///  skVal  -----  Access key secret
func CalcSig(content []byte, contentType string,
	dateStr string, key string, method string, skVal string) string {

	h := md5.New()
	h.Write(content)

	cipherStr := h.Sum(nil)

	mac := hmac.New(sha1.New, []byte(skVal))
	mac.Write([]byte(method + "\n" +
		hex.EncodeToString(cipherStr) + "\n" +
		contentType + "\n" +
		dateStr + "\n" +
		key))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	//log.Println("sig", sig)
	return sig
}

// Store sends a batch of samples to the HTTP endpoint.
var (
	storeTotal = 0
)

type issueResp struct {
	Code      int    `json:"code"`
	ErrorCode string `json:"errorCode"`
	Message   string `json:"message"`
}

func CreateIssueSource(check bool) error {

	data := []byte("")
	compressed := snappy.Encode(nil, data)

	requrl := cfg.Cfg.RemoteHost
	if requrl[len(requrl)-1] == '/' {
		requrl = requrl[:len(requrl)-1]
	}

	if check {
		requrl = requrl + fmt.Sprintf("/v1/uploader-uid/check?uploader_uid=%s", cfg.Cfg.UploaderUID)
	} else {
		requrl = requrl + "/v1/issue-source"
	}

	httpReq, err := http.NewRequest("POST", requrl, bytes.NewReader(compressed))
	if err != nil {
		return err
	}

	contentType := "application/x-protobuf"
	contentEncode := "snappy"
	date := time.Now().UTC().Format(http.TimeFormat)

	sig := CalcSig(nil, contentType,
		date, cfg.Cfg.TeamID, http.MethodPost, cfg.DecodedSK)

	log.Println("[info] hostname:", HostName)

	httpReq.Header.Set("Content-Encoding", contentEncode)
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("X-Version", cfg.ProbeName+"/"+git.Version)
	httpReq.Header.Set("X-Team-Id", cfg.Cfg.TeamID)
	httpReq.Header.Set("X-Uploader-Uid", cfg.Cfg.UploaderUID)
	httpReq.Header.Set("X-Uploader-Ip", cfg.Cfg.Host)
	httpReq.Header.Set("X-Host-Name", HostName)
	httpReq.Header.Set("X-App-Name", cfg.ProbeName)
	httpReq.Header.Set("Date", date)
	httpReq.Header.Set("Authorization", "kodo "+cfg.Cfg.AK+":"+sig)

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if check && httpResp.StatusCode == 200 {
		return nil
	}

	if httpResp.StatusCode == 404 {
		return fmt.Errorf("page not found: %s", requrl)
	}

	resdata, err := ioutil.ReadAll(httpResp.Body)
	if err != nil {
		return err
	}

	var m issueResp

	err = json.Unmarshal(resdata, &m)
	if err != nil {
		return err
	}

	if m.Code != 200 {
		log.Printf("resp: %s", string(resdata))
		return fmt.Errorf("%s", m.Message)
	}
	return nil
}

// Store sends a batch of samples to the HTTP endpoint.
func (c *Client) Store(ctx context.Context, req *prompb.WriteRequest) error {

	data, err := proto.Marshal(req)
	if err != nil {
		log.Printf("[error] %s", err.Error())
		return err
	}

	//level.Info(c.logger).Log("msg", "----send", len(data), len(data))

	compressed := snappy.Encode(nil, data)
	httpReq, err := http.NewRequest("POST", c.url.String(), bytes.NewReader(compressed))
	if err != nil {
		// Errors from NewRequest are from unparseable URLs, so are not
		// recoverable.
		log.Printf("[error] %s", err.Error())
		return err
	}

	//level.Debug(c.logger).Log("msg", "snappy ratio", len(compressed), len(data))

	contentType := "application/x-protobuf"
	contentEncode := "snappy"
	date := time.Now().UTC().Format(http.TimeFormat)

	sig := CalcSig(compressed, contentType,
		date, cfg.Cfg.TeamID, http.MethodPost, cfg.DecodedSK)

	//log.Println("HostName：", HostName)

	httpReq.Header.Set("Content-Encoding", contentEncode)
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("X-Version", cfg.ProbeName+"/"+git.Version)
	httpReq.Header.Set("X-Team-Id", cfg.Cfg.TeamID)
	httpReq.Header.Set("X-Uploader-Uid", cfg.Cfg.UploaderUID)
	httpReq.Header.Set("X-Uploader-Ip", cfg.Cfg.Host)
	httpReq.Header.Set("X-Host-Name", HostName)
	httpReq.Header.Set("X-App-Name", cfg.ProbeName)
	httpReq.Header.Set("Date", date)
	httpReq.Header.Set("Authorization", "kodo "+cfg.Cfg.AK+":"+sig)
	httpReq = httpReq.WithContext(ctx)

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	httpResp, err := ctxhttp.Do(ctx, c.client, httpReq)
	if err != nil {
		// Errors from client.Do are from (for example) network errors, so are
		// recoverable.
		return recoverableError{err}
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode/100 != 2 {
		if errdata, err := ioutil.ReadAll(httpReq.Body); err == nil {
			var resJson issueResp
			if json.Unmarshal(errdata, &resJson) == nil {
				if resJson.Code == 403 {
					os.Exit(1024)
				}
			}
		}

		// scanner := bufio.NewScanner(io.LimitReader(httpResp.Body, maxErrMsgLen))
		// line := []byte(`{"error": "", "rejected": false, "msg": ""}`)

		// if scanner.Scan() {
		// 	line = scanner.Bytes()
		// 	var msg KodoMsg

		// 	if err := json.Unmarshal(line, &msg); err != nil {
		// 		// pass
		// 	} else {
		// 		if msg.Rejected {
		// 			log.Printf("[fatal] rejected by kodo: %s", msg.Error)
		// 			os.Exit(-1)
		// 		}
		// 	}

		// }

		err = fmt.Errorf("server returned HTTP status %s", httpResp.Status)
	}
	if httpResp.StatusCode/100 == 5 {
		return recoverableError{err}
	}
	return err
}

// Name identifies the client.
func (c Client) Name() string {
	return fmt.Sprintf("%d:%s", c.index, c.url)
}
