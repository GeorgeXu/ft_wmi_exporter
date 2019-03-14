// +build windows

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"

	"wmi_exporter/cfg"
	"wmi_exporter/cloudcare"
	"wmi_exporter/collector"
	"wmi_exporter/envinfo"
	"wmi_exporter/git"

	"github.com/StackExchange/wmi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	//"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
	uuid "github.com/satori/go.uuid"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

// WmiCollector implements the prometheus.Collector interface.
type WmiCollector struct {
	collectors map[string]collector.Collector
}

type collectorItem struct {
	name    string
	enabled bool
}

var (
	collectorItemList []collectorItem
	errorCollectors   = make(map[string]int)
)

const (
	defaultCollectors            = "cpu,cs,logical_disk,net,os,service,system,textfile"
	defaultCollectorsPlaceholder = "[defaults]"
	serviceName                  = "wmi_exporter"
)

var (
	collectorState = make(map[string]bool)
)

var (
	scrapeDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(collector.Namespace, "exporter", "collector_duration_seconds"),
		"wmi_exporter: Duration of a collection.",
		[]string{"collector"},
		nil,
	)
	scrapeSuccessDesc = prometheus.NewDesc(
		prometheus.BuildFQName(collector.Namespace, "exporter", "collector_success"),
		"wmi_exporter: Whether the collector was successful.",
		[]string{"collector"},
		nil,
	)

	// This can be removed when client_golang exposes this on Windows
	// (See https://github.com/prometheus/client_golang/issues/376)
	startTime     = float64(time.Now().Unix())
	startTimeDesc = prometheus.NewDesc(
		"process_start_time_seconds",
		"Start time of the process since unix epoch in seconds.",
		nil,
		nil,
	)
)

// Describe sends all the descriptors of the collectors included to
// the provided channel.
func (coll WmiCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- scrapeDurationDesc
	ch <- scrapeSuccessDesc
}

// Collect sends the collected metrics from each of the collectors to
// prometheus. Collect could be called several times concurrently
// and thus its run is protected by a single mutex.
func (coll WmiCollector) Collect(ch chan<- prometheus.Metric) {
	wg := sync.WaitGroup{}
	wg.Add(len(coll.collectors))
	for name, c := range coll.collectors {
		go func(name string, c collector.Collector) {
			execute(name, c, ch)
			wg.Done()
		}(name, c)
	}

	ch <- prometheus.MustNewConstMetric(
		startTimeDesc,
		prometheus.CounterValue,
		startTime,
	)
	wg.Wait()
}

func filterAvailableCollectors(collectors string) string {
	var availableCollectors []string
	for _, c := range strings.Split(collectors, ",") {
		_, ok := collector.Factories[c]
		if ok {
			availableCollectors = append(availableCollectors, c)
		}
	}
	return strings.Join(availableCollectors, ",")
}

func execute(name string, c collector.Collector, ch chan<- prometheus.Metric) {

	// ncerr := 0
	// if nc, ok := errorCollectors[name]; ok {
	// 	ncerr = nc
	// }
	// if ncerr > 3 {
	// 	return
	// }

	begin := time.Now()
	err := c.Collect(ch)
	duration := time.Since(begin)
	var success float64

	if err != nil {
		log.Printf("collector %s failed after %fs: %s", name, duration.Seconds(), err)
		success = 0
		//ncerr++
		//errorCollectors[name] = ncerr

	} else {
		//log.Printf("collector %s succeeded after %fs.", name, duration.Seconds())
		success = 1
		//errorCollectors[name] = 0
	}
	ch <- prometheus.MustNewConstMetric(
		scrapeDurationDesc,
		prometheus.GaugeValue,
		duration.Seconds(),
		name,
	)
	ch <- prometheus.MustNewConstMetric(
		scrapeSuccessDesc,
		prometheus.GaugeValue,
		success,
		name,
	)
}

func setEnableCollectors(enables string) {

	if enables == "all" {
		for _, v := range collectorItemList {
			v.enabled = true
		}
	} else {
		parts := strings.Split(enables, "-")
		var nums []uint32
		for _, v := range parts {
			n, _ := strconv.ParseUint(v, 16, 32)
			nums = append(nums, uint32(n))
		}

		for index := 0; index < len(collectorItemList); index++ {

			n := nums[index/32]
			flagbit := uint32(1 << uint(index%32))

			collectorItemList[index].enabled = ((n & flagbit) == flagbit)
		}
	}
}

func loadCollectors() (map[string]collector.Collector, error) {

	collectors := map[string]collector.Collector{}

	for name, enable := range cfg.Cfg.Collectors {
		if enable {
			if fn, ok := collector.Factories[name]; ok {
				c, err := fn()
				if err == nil {
					collectors[name] = c
				}
			}
		}
	}
	return collectors, nil
}

func init() {

	collectorItemList = []collectorItem{
		collectorItem{"ad", true},
		collectorItem{"cpu", true},
		collectorItem{"cs", true},
		collectorItem{"dns", true},
		collectorItem{"hyperv", true},
		collectorItem{"iis", true},
		collectorItem{"logical_disk", true},
		collectorItem{"memory", true},
		collectorItem{"msmq", true},
		collectorItem{"mssql", true},
		collectorItem{"netframework_clrexceptions", true},
		collectorItem{"netframework_clrinterop", true},
		collectorItem{"netframework_clrjit", true},
		collectorItem{"netframework_clrloading", true},
		collectorItem{"netframework_clrlocksandthreads", true},
		collectorItem{"netframework_clrmemory", true},
		collectorItem{"netframework_clrremoting", true},
		collectorItem{"netframework_clrsecurity", true},
		collectorItem{"net", true},
		collectorItem{"os", true},
		collectorItem{"process", true},
		collectorItem{"service", true},
		collectorItem{"system", true},
		collectorItem{"tcp", true},
		collectorItem{"vmware", true},
	}

}

func initWbem() {
	// This initialization prevents a memory leak on WMF 5+. See
	// https://github.com/martinlindhe/wmi_exporter/issues/77 and linked issues
	// for details.
	log.Println("Initializing SWbemServices")
	s, err := wmi.InitializeSWbemServices(wmi.DefaultClient)
	if err != nil {
		log.Fatal(err)
	}
	wmi.DefaultClient.AllowMissingFields = true
	wmi.DefaultClient.SWbemServicesClient = s
}

var (
	metricsPath  = kingpin.Flag("web.telemetry.path", "URL path for surfacing collected metrics.").Default("/metrics").String()
	envinfoPath  = kingpin.Flag("web.telemetry.envpath", "URL path for surfacing collected envinfo.").Default("/env_infos").String()
	fileInfoPath = kingpin.Flag("web.telemetry-file-info-path", "Path under which to expose file info.").Default("/file_infos").String()
	metaPath     = kingpin.Flag("web.meta-path", "Path under which to expose meta info.").Default("/meta").String()

	// enabledCollectors = kingpin.Flag(
	// 	"collectors.enabled",
	// 	"Comma-separated list of collectors to use. Use '[defaults]' as a placeholder for all the collectors enabled by default.").
	// 	Default(filterAvailableCollectors(defaultCollectors)).String()
	printCollectors = kingpin.Flag("collectors.print", "If true, print available collectors and exit.").Bool()

	flagInit      = kingpin.Flag("init", `init config on insyall`).Bool()
	flagUpdateCfg = kingpin.Flag("update-cfg", `update config from ui`).Bool()

	flagSingleMode = kingpin.Flag("single-mode", "run as single node").Default(fmt.Sprintf("%d", cfg.Cfg.SingleMode)).Int()

	flagTeamID              = kingpin.Flag("team-id", "User ID").String()
	flagAK                  = kingpin.Flag("ak", `Access Key`).String()
	flagSK                  = kingpin.Flag("sk", `Secret Key`).String()
	flagHostIP              = kingpin.Flag("host", `eg. ip addr`).String()
	flagPort                = kingpin.Flag("port", `listen port`).Int()
	flagEnableAllCollectors = kingpin.Flag("enabled", `enabled collectors`).String()
	//flagEnableAll           = kingpin.Flag("enable-all", "enable all collectors").Default(fmt.Sprintf("%d", cfg.Cfg.EnableAll)).Int()

	flagUploaderUID = kingpin.Flag("uploader-uid", "uuid").String()

	flagRemoteHost = kingpin.Flag("remote-host", `data bridge addr`).String()

	flagRemoteMetricsWritePath = kingpin.Flag("metric-path", ``).Default("v1/write").String()
	flagRemoteEnvWritePath     = kingpin.Flag("env-path", ``).Default("v1/write/env").String()

	flagScrapeInterval         = kingpin.Flag("scrape-interval", "frequency to upload data").Default(fmt.Sprintf("%d", cfg.Cfg.ScrapeMetricInterval)).Int()
	flagEnvInfoScrapeInterval  = kingpin.Flag("scrape-env-interval", "frequency to upload env info").Default(fmt.Sprintf("%d", cfg.Cfg.ScrapeEnvInfoInterval)).Int()
	flagScrapeFileInfoInterval = kingpin.Flag("scrape-file-info-interval", "frequency to upload file info data(ms)").Default(fmt.Sprintf("%d", cfg.Cfg.ScrapeFileInfoInterval)).Int()

	flagVersionInfo = kingpin.Flag("version", "show version info").Bool()

	flagProvider = kingpin.Flag("provider", "cloud service provider").Default("aliyun").String()
)

func updateCfg() error {
	var err error
	err = cfg.LoadConfig()
	if err != nil {
		log.Fatalf("[fatal] load config failed: %s", err)
	}

	if *flagTeamID != "" {
		cfg.Cfg.TeamID = *flagTeamID
	}
	if *flagAK != "" {
		cfg.Cfg.AK = *flagAK
	}
	if *flagSK != "" {
		cfg.Cfg.SK = cfg.XorEncode(*flagSK)
		cfg.DecodedSK = *flagSK
	}
	if *flagPort != 0 {
		cfg.Cfg.Port = *flagPort
	}
	if *flagHostIP != "" {
		cfg.Cfg.Host = *flagHostIP
	}
	if *flagRemoteHost != "" {
		cfg.Cfg.RemoteHost = *flagRemoteHost
	}
	if *flagEnableAllCollectors != "" {
		setEnableCollectors(*flagEnableAllCollectors)

		cfg.Cfg.Collectors = make(map[string]bool)
		for _, v := range collectorItemList {
			cfg.Cfg.Collectors[v.name] = v.enabled
		}
	}

	//checkPort(cfg.Cfg.Port)

	if err := cloudcare.CreateIssueSource(false); err != nil {
		log.Printf("check err: %s", err)
		errpath := filepath.Join(filepath.Dir(os.Args[0]), "install_error")
		ioutil.WriteFile(errpath, []byte(err.Error()), 0666)
		os.Exit(1024)
	}

	err = cfg.DumpConfig()
	if err != nil {
		log.Fatalf("[fatal] dump config failed: %s", err)
	}

	return nil
}

func initCfg() error {

	if *flagTeamID == "" {
		log.Fatalln("[fatal] invalid team-id")
	}
	if *flagAK == "" {
		log.Fatalln("[fatal] invalid AK")
	}
	if *flagSK == "" {
		log.Fatalln("[fatal] invalid SK")
	}

	cfg.Cfg.SingleMode = *flagSingleMode
	if *flagHostIP != "" {
		cfg.Cfg.Host = *flagHostIP
	}

	if *flagRemoteHost != "" {
		cfg.Cfg.RemoteHost = *flagRemoteHost
	}
	cfg.Cfg.ScrapeEnvInfoInterval = *flagEnvInfoScrapeInterval
	cfg.Cfg.ScrapeFileInfoInterval = *flagScrapeFileInfoInterval
	cfg.Cfg.ScrapeMetricInterval = *flagScrapeInterval
	//cfg.Cfg.EnableAll = *flagEnableAll

	cfg.Cfg.TeamID = *flagTeamID
	cfg.Cfg.AK = *flagAK
	cfg.Cfg.SK = cfg.XorEncode(*flagSK)
	cfg.DecodedSK = *flagSK

	if *flagPort != 0 {
		cfg.Cfg.Port = *flagPort
	}
	cfg.Cfg.Provider = *flagProvider

	setEnableCollectors(*flagEnableAllCollectors)

	cfg.Cfg.Collectors = make(map[string]bool)
	for _, v := range collectorItemList {
		cfg.Cfg.Collectors[v.name] = v.enabled
	}

	bcheck := false
	if *flagUploaderUID != "" {
		cfg.Cfg.UploaderUID = *flagUploaderUID
		bcheck = true
	} else {
		// 客户端自行生成 ID, 而不是 kodo 下发
		uid, err := uuid.NewV4()
		if err != nil {
			log.Fatal(err)
		}
		cfg.Cfg.UploaderUID = fmt.Sprintf("uid-%s", uid.String())
	}

	checkPort(cfg.Cfg.Port)

	if err := cloudcare.CreateIssueSource(bcheck); err != nil {
		log.Printf("init check err: %s", err)
		errpath := filepath.Join(filepath.Dir(os.Args[0]), "install_error")
		ioutil.WriteFile(errpath, []byte(err.Error()), 0666)
		os.Exit(1024)
	}

	return cfg.DumpConfig()
}

func checkPort(port int) error {
	chkconn, _ := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if chkconn != nil {
		chkconn.Close()
		log.Printf("[error] port %s has been used", port)
		errpath := filepath.Join(filepath.Dir(os.Args[0]), "install_error")
		ioutil.WriteFile(errpath, []byte("carrier.kodo.portused"), 0666)
		os.Exit(1024)
	}
	return nil
}

func main() {

	//log.AddFlags(kingpin.CommandLine)
	//kingpin.Version(version.Print("Corsair"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	if *flagVersionInfo {
		fmt.Printf(`Version:        %s
Sha1:           %s
Build At:       %s
Golang Version: %s
`, git.Version, git.Sha1, git.BuildAt, git.Golang)
		return
	}

	logfilepath := filepath.Join(filepath.Dir(os.Args[0]), cfg.ProbeName+".log")
	rw, err := cloudcare.SetLog(logfilepath)
	if err != nil {
		log.Fatal(err)
	}
	defer rw.Close()

	if *flagInit {
		initCfg()
		return
	} else if *flagUpdateCfg {
		updateCfg()
		return
	}

	if err := cfg.LoadConfig(); err != nil {
		log.Fatalf("[error] load config fail: %s", err)
	}

	cfg.DumpConfig()

	checkPort(cfg.Cfg.Port)

	if cfg.Cfg.SingleMode > 0 {
		if err := cloudcare.CreateIssueSource(false); err != nil {
			log.Printf("check err: %s", err)
			errpath := filepath.Join(filepath.Dir(os.Args[0]), "install_error")
			ioutil.WriteFile(errpath, []byte(err.Error()), 0666)
			os.Exit(1024)
		}
	}

	if *printCollectors {
		collectorNames := make(sort.StringSlice, 0, len(collector.Factories))
		for n := range collector.Factories {
			collectorNames = append(collectorNames, n)
		}
		collectorNames.Sort()
		fmt.Printf("Available collectors:\n")
		for _, n := range collectorNames {
			fmt.Printf(" - %s\n", n)
		}
		return
	}

	collectors, err := loadCollectors()
	if err != nil {
		log.Fatalf("%s", err)
	}

	initWbem()

	isInteractive, err := svc.IsAnInteractiveSession()
	if err != nil {
		log.Fatal(err)
	}

	stopCh := make(chan bool)
	if !isInteractive {
		go svc.Run(serviceName, &wmiExporterService{stopCh: stopCh})
	}

	log.Printf("Enabled metric collectors: %v", strings.Join(keys(collectors), ", "))

	version.Version = git.Version
	version.Revision = git.Sha1
	version.Branch = "master"
	version.BuildDate = git.BuildAt
	prometheus.MustRegister(version.NewCollector(cfg.ProbeName))

	nodeCollector := WmiCollector{collectors: collectors}
	prometheus.MustRegister(nodeCollector)

	// init envinfo configure
	envinfo.OSQuerydPath = filepath.Join(filepath.Dir(os.Args[0]), `osqueryd.exe`)
	envinfo.Init(filepath.Join(filepath.Dir(os.Args[0]), `env.json`))

	envRegister := prometheus.NewRegistry()
	envcCollector := envinfo.NewEnvInfoCollector()
	envRegister.MustRegister(envcCollector)

	http.Handle(*metricsPath, promhttp.Handler())
	http.Handle(*envinfoPath, promhttp.HandlerFor(envRegister, promhttp.HandlerOpts{}))

	http.HandleFunc(*metaPath, func(w http.ResponseWriter, r *http.Request) {
		hostName, err := os.Hostname()
		if err != nil {
			//log.Printf("[error] %s, ignored", err.Error())
		}
		j, err := json.Marshal(&cfg.Meta{
			UploaderUID: cfg.Cfg.UploaderUID,
			HostName:    hostName,
		})
		if err != nil {
			log.Printf("[error] %s, ignored", err.Error())
			fmt.Fprintf(w, err.Error())
		} else {
			fmt.Fprintf(w, string(j))
		}
	})

	http.HandleFunc("/health", healthCheck)
	http.HandleFunc("/collectors", func(w http.ResponseWriter, r *http.Request) {
		s := ""
		for _, item := range collectorItemList {
			k := item.name
			if v, ok := cfg.Cfg.Collectors[item.name]; ok {
				s += fmt.Sprintf("%s = %v", k, v)
				s += "\n"
			}
		}
		w.Write([]byte(s))
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, *metricsPath, http.StatusMovedPermanently)
	})

	log.Println(fmt.Sprintf("Starting Corsair on %d", cfg.Cfg.Port), version.Info())
	//log.Infoln("Build context", version.BuildContext())

	if cfg.Cfg.SingleMode == 1 {

		if cfg.Cfg.SingleMode == 1 {
			// metric 数据收集和上报
			metricsScrapeUrl := fmt.Sprintf("http://localhost:%d%s", cfg.Cfg.Port, *metricsPath)
			postURLMetric := fmt.Sprintf("%s%s", cfg.Cfg.RemoteHost, "/v1/write")

			log.Printf("[debug] metric url: %s", metricsScrapeUrl)

			if err := cloudcare.Start(postURLMetric, metricsScrapeUrl, cfg.Cfg.ScrapeMetricInterval); err != nil {
				log.Fatalf("[fatal] %s", err)
			}

			// env info 收集器
			envScrapeUrl := fmt.Sprintf("http://localhost:%d%s", cfg.Cfg.Port, *envinfoPath)
			postURLEnv := fmt.Sprintf("%s%s", cfg.Cfg.RemoteHost, "/v1/write/env")

			log.Printf("[debug] env-info url: %s", envScrapeUrl)

			if err := cloudcare.Start(postURLEnv, envScrapeUrl, cfg.Cfg.ScrapeEnvInfoInterval); err != nil {
				log.Fatalf("[fatal] %s", err)
			}

			// file info 收集器
			// fileinfoScrapeUrl := fmt.Sprintf("http://0.0.0.0:%d%s", cfg.Cfg.Port, *fileInfoPath)
			// postURLFile := fmt.Sprintf("%s%s", cfg.Cfg.RemoteHost, "/v1/write/env")

			// log.Printf("[debug] env-info url: %s", fileinfoScrapeUrl)

			// if err := cloudcare.Start(postURLFile, fileinfoScrapeUrl, cfg.Cfg.ScrapeFileInfoInterval); err != nil {
			// 	log.Fatalf("[fatal] %s", err)
			// }

			// TODO: 这些主动上报收集器, 并入集群模式时, 需要设计退出机制
		}

	}

	go func() {
		listenAddress := fmt.Sprintf("%s:%d", cfg.Cfg.BindAddr, cfg.Cfg.Port)
		if err := http.ListenAndServe(listenAddress, nil); err != nil {
			log.Printf("[fatal] %s", err.Error())
			errpath := filepath.Join(filepath.Dir(os.Args[0]), "install_error")
			ioutil.WriteFile(errpath, []byte(err.Error()), 0666)
			os.Exit(1024)
		}
	}()

	for {
		if <-stopCh {
			log.Println("Shutting down Corsair")
			break
		}
	}

}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"status":"ok"}`)
}

func keys(m map[string]collector.Collector) []string {
	ret := make([]string, 0, len(m))
	for key := range m {
		ret = append(ret, key)
	}
	return ret
}

type wmiExporterService struct {
	stopCh chan<- bool
}

func (s *wmiExporterService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s.stopCh <- true
				break loop
			default:
				log.Println(fmt.Sprintf("unexpected control request #%d", c))
			}
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
}
