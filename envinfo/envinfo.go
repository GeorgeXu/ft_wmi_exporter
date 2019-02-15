package envinfo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"

	yaml "gopkg.in/yaml.v2"
)

const (
	fetchType_osquery = "osquery"
	fetchType_cat     = "cat"
	fetchType_wmi     = "wmi"
	fetchType_custom  = "custom"
)

var oq *osquery

type osquery struct {
	exepath   string
	cfgpath   string
	avariable bool
}

func InitOsquery(path string) error {

	oq = &osquery{
		avariable: false,
	}

	folder := filepath.Dir(path)
	oq.exepath = filepath.Join(folder, "osqueryd.exe")
	oq.cfgpath = filepath.Join(folder, "envcfg.yml")

	err := LoadConfig(oq.cfgpath)
	if err == nil {
		oq.avariable = true
	}

	return err
}

type envInfoError struct {
	Err string `json:"error"`
}

func (q *osquery) query(sql string) (string, error) {

	var e *envInfoError
	if q.exepath == "" {
		//for test
		e = &envInfoError{
			Err: "cannnot find osquery",
		}
	}

	cmd := exec.Command(q.exepath, "-S", "--json", sql)
	result, err := cmd.Output()

	if err != nil {
		e = &envInfoError{
			Err: fmt.Sprintf("%s", err),
		}
	}
	if e != nil {
		jbs, _ := json.Marshal(e)
		return base64.RawURLEncoding.EncodeToString(jbs), err
	}
	return base64.RawURLEncoding.EncodeToString(result), nil
}

func LoadConfig(f string) error {

	infos := []*EnvQueryInfo{}

	data, err := ioutil.ReadFile(f)
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(data, &infos); err != nil {
		return err
	}

	queryInfos = infos

	for _, info := range queryInfos {
		c, err := NewEnvCustomCollector([]*EnvQueryInfo{info})
		if err == nil {
			factories[info.fullName()] = c
			collectorState[info.fullName()] = info.Enable
		}
	}

	return nil
}

// 当前配置写入配置文件
func DumpConfig(f string) error {

	c, err := yaml.Marshal(&queryInfos)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(f, c, 0644)
}
