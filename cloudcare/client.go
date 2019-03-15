package cloudcare

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	Message   string `json:"message,omitempty"`
}

type versionResData struct {
	DownloadUrl string `json:"download_url"`
	Single      bool   `json:"single"`
	Size        int    `json:"size"`
	UploaderID  string `json:"uploader_id,omitempty"`
	AK          string `json:"ak,omitempty"`
}

type versionResponse struct {
	Code      int            `json:"code"`
	ErrorCode string         `json:"errorCode"`
	Message   string         `json:"message,omitempty"`
	Content   versionResData `json:"content,omitempty"`
}

func GetNewVersionAddr() error {

	//data := []byte("")
	//compressed := snappy.Encode(nil, data)

	requrl := cfg.Cfg.RemoteHost
	if requrl[len(requrl)-1] == '/' {
		requrl = requrl[:len(requrl)-1]
	}

	requrl = requrl + "/v1/probe/install?platform=windows&name=corsair"

	httpReq, err := http.NewRequest("GET", requrl, nil)
	if err != nil {
		return err
	}

	contentType := "application/x-protobuf"
	contentEncode := "snappy"
	date := time.Now().UTC().Format(http.TimeFormat)

	sig := CalcSig(nil, contentType,
		date, cfg.Cfg.TeamID, http.MethodGet, cfg.DecodedSK)

	//log.Println("[info] hostname:", HostName)

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

	if httpResp.StatusCode == 404 {
		return fmt.Errorf("page not found: %s", requrl)
	}

	if httpResp.StatusCode == 200 {

		var r versionResponse

		resdata, err := ioutil.ReadAll(httpResp.Body)

		if err != nil {
			return err
		}

		if err := json.Unmarshal(resdata, &r); err != nil {
			return err
		}

		log.Println("downloadurl:", r.Content.DownloadUrl)

		urlpath := filepath.Join(filepath.Dir(os.Args[0]), "dowload_url")
		ioutil.WriteFile(urlpath, []byte(r.Content.DownloadUrl), 0666)

		return nil
	}

	err = fmt.Errorf("%s status: %s", httpReq.URL.Path, httpResp.Status)
	log.Println(err.Error())

	return err
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

	if httpResp.StatusCode == 200 {
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
		err = fmt.Errorf("%s status: %s, body: %s", httpReq.URL.Path, httpResp.Status, string(resdata))
		log.Printf(err.Error())
		return err
	}

	if m.Code != 200 {
		log.Printf("%s status: %s, body: %s", httpReq.URL.Path, httpResp.Status, string(resdata))
		return fmt.Errorf("%s", m.ErrorCode)
	}

	return nil
}

var ErrorCodeRejected = "carrier.kodo.rejected"

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

		scanner := bufio.NewScanner(io.LimitReader(httpResp.Body, maxErrMsgLen))
		line := []byte(`{"code": "", "errorCode": "", "message": ""}`)

		if scanner.Scan() {
			line = scanner.Bytes()
			var msg issueResp

			if err := json.Unmarshal(line, &msg); err != nil {
				// pass
			} else {
				if msg.ErrorCode == ErrorCodeRejected {
					log.Printf("[fatal] rejected by kodo: %s", msg.Message)
					os.Exit(1024)
				}
			}
		}
		err = fmt.Errorf("server returned HTTP status %s: %s", httpResp.Status, string(line))

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
