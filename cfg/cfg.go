package cfg

import (
	"bytes"
	"encoding/base64"
	"io/ioutil"
	"math/rand"
	"time"

	yaml "gopkg.in/yaml.v2"

	"wmi_exporter/cloudcare"
	"wmi_exporter/collector"

	"github.com/prometheus/common/model"
)

type Config struct {
	TeamID         string          `yaml:"team_id"`
	CloudAssetID   string          `yaml:"cloud_asset_id"`
	AK             string          `yaml:"ak"`
	SK             string          `yaml:"sk"`
	Port           int             `yaml:"port"`
	Collectors     map[string]bool `yaml:"collectors"`
	SingleMode     int             `yaml:"single_mode"`
	Host           string          `yaml:"host"`
	ScrapeInterval int             `yaml:"scrap_interval"`
	RemoteHost     string          `yaml:"remote_host"`
	EnableAll      int             `yaml:"enable_all"`
}

var (
	Cfg Config
)

// 导入 @f 中的配置
func LoadConfig(f string) error {
	data, err := ioutil.ReadFile(f)
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(data, &Cfg); err != nil {
		return err
	}

	if err := initPromCfg(&Cfg); err != nil {
		return err
	}
	return nil
}

// 当前配置写入配置文件
func DumpConfig(f string) error {
	c, err := yaml.Marshal(&Cfg)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(f, c, 0644)
}

// 对用户的 SecretKey 进行加密
var xorkeys = []byte{
	0xbb, 0x74, 0x24, 0xa5,
	0xba, 0x5a, 0x0a, 0x8c,
	0x65, 0x61, 0xdf, 0x57,
	0xa1, 0x3c, 0xfb, 0xe9,
	0x89, 0x12, 0xcb, 0x5a,
	0xd2, 0x70, 0xf3, 0x82,
	0x67, 0xdd, 0x5c, 0x8a,
	0xec, 0x77, 0xcf, 0x48,
	0x39, 0x1c, 0x0e, 0xab,
	0xee, 0xe, 0x16, 0xe8,
	0x2c, 0xab, 0xf2, 0x61,
	0xfc, 0xc7, 0xfd, 0x1c,
	0x58, 0xfc, 0xe7, 0x4f,
	0x70, 0xed, 0xc8, 0xf1,
	0x5f, 0x36, 0x18, 0x3c,
	0x29, 0x38, 0x27, 0xc1,
	0xbc, 0x29, 0x3, 0x89,
	0xcb, 0xbe, 0xc7, 0xc8,
	0xce, 0xb3, 0x7d, 0x7d,
	0xe1, 0x84, 0x74, 0xd, 0x1c, 0x66, 0xb6, 0x86, 0xbc, 0xb, 0x33, 0x1, 0x17, 0x93, 0xd3, 0x82, 0xb7, 0xb0, 0x96, 0xe3, 0xd6, 0xef, 0xc4, 0xa1, 0xf7, 0xb0, 0x6e, 0xd, 0x55, 0x2e, 0x3e, 0x25, 0x4c, 0xf7, 0xc6, 0xeb, 0x63, 0x8c, 0x88, 0x69, 0xf5, 0x86, 0x6a, 0x56, 0xc1, 0xaf, 0x46, 0xbf, 0x6f, 0x35, 0xfc, 0x90}

func XorEncode(sk string) string {

	r := rand.New(rand.NewSource(time.Now().Unix()))

	var msg bytes.Buffer

	msg.WriteByte(byte(len(sk)))
	msg.Write([]byte(sk))
	for {
		if msg.Len() >= 128 {
			break
		}
		msg.WriteByte(byte(r.Intn(255)))
	}

	var en bytes.Buffer
	for index := 0; index < msg.Len(); index++ {
		en.WriteByte(msg.Bytes()[index] ^ xorkeys[index])
	}

	return base64.StdEncoding.EncodeToString(en.Bytes())
}

func xorDecode(endata string) []byte {
	data, err := base64.StdEncoding.DecodeString(endata)
	if err != nil {
		return nil
	}
	length := data[0] ^ xorkeys[0]

	var dedata bytes.Buffer
	for index := 0; index < 128; index++ {
		dedata.WriteByte(data[index] ^ xorkeys[index])
	}
	return dedata.Bytes()[1 : 1+length]
}

// 从现有配置中初始化 prom 的配置
func initPromCfg(c *Config) error {

	if c.Host != "" {
		cloudcare.CorsairHost = c.Host
	}
	if c.TeamID != "" {
		cloudcare.CorsairTeamID = c.TeamID
	}
	if c.AK != "" {
		cloudcare.CorsairAK = c.AK
	}
	if c.SK != "" {
		cloudcare.CorsairSK = string(xorDecode(c.SK))
	}

	cloudcare.CorsairPort = c.Port

	if c.CloudAssetID != "" {
		cloudcare.CorsairCloudAssetID = c.CloudAssetID
	}

	if c.EnableAll == 1 {
		// 开启所有收集器
		for k, _ := range c.Collectors {
			collector.SetCollector(k, true)
		}
	} else {
		// 将配置中的开关设置到 collector 模块中
		for k, v := range c.Collectors {
			collector.SetCollector(k, v)
		}
	}

	cloudcare.PromCfg.GlobalConfig.ScrapeInterval =
		model.Duration(c.ScrapeInterval) * model.Duration(time.Second)

	return nil
}
