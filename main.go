// +build windows

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
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

var collectorItemList []collectorItem

const (
	defaultCollectors            = "cpu,cs,logical_disk,net,os,service,system,textfile"
	defaultCollectorsPlaceholder = "[defaults]"
	serviceName                  = "wmi_exporter"
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

func setEnableCollectors(enables string) {

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
	begin := time.Now()
	err := c.Collect(ch)
	duration := time.Since(begin)
	var success float64

	if err != nil {
		log.Errorf("collector %s failed after %fs: %s", name, duration.Seconds(), err)
		success = 0
	} else {
		log.Debugf("collector %s succeeded after %fs.", name, duration.Seconds())
		success = 1
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

func loadEnableCollectors() []string {
	var result []string
	if cfg.Cfg.EnableAll > 0 {
		for k, _ := range collector.Factories {
			result = append(result, k)
		}
	} else {
		for k, v := range cfg.Cfg.Collectors {
			if v {
				result = append(result, k)
			}
		}
	}

	return result
}

func expandEnabledCollectors(enabled string) []string {
	expanded := strings.Replace(enabled, defaultCollectorsPlaceholder, defaultCollectors, -1)
	separated := strings.Split(expanded, ",")
	unique := map[string]bool{}
	for _, s := range separated {
		if s != "" {
			unique[s] = true
		}
	}
	result := make([]string, 0, len(unique))
	for s := range unique {
		result = append(result, s)
	}
	return result
}

func loadCollectors(list []string) (map[string]collector.Collector, error) {

	collectors := map[string]collector.Collector{}

	for _, name := range list {
		fn, ok := collector.Factories[name]
		if !ok {
			return nil, fmt.Errorf("collector '%s' not available", name)
		}
		c, err := fn()
		if err != nil {
			return nil, err
		}
		collectors[name] = c
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

	prometheus.MustRegister(version.NewCollector("wmi_exporter"))
}

func initWbem() {
	// This initialization prevents a memory leak on WMF 5+. See
	// https://github.com/martinlindhe/wmi_exporter/issues/77 and linked issues
	// for details.
	log.Debugf("Initializing SWbemServices")
	s, err := wmi.InitializeSWbemServices(wmi.DefaultClient)
	if err != nil {
		log.Fatal(err)
	}
	wmi.DefaultClient.AllowMissingFields = true
	wmi.DefaultClient.SWbemServicesClient = s
}

var (
	metricsPath = kingpin.Flag(
		"telemetry.path",
		"URL path for surfacing collected metrics.",
	).Default("/metrics").String()

	envinfoPath = kingpin.Flag(
		"telemetry.envpath",
		"URL path for surfacing collected envinfo.",
	).Default("/env_infos").String()

	metaPath = kingpin.Flag("web.meta-path", "Path under which to expose meta info.").Default("/meta").String()

	// enabledCollectors = kingpin.Flag(
	// 	"collectors.enabled",
	// 	"Comma-separated list of collectors to use. Use '[defaults]' as a placeholder for all the collectors enabled by default.").
	// 	Default(filterAvailableCollectors(defaultCollectors)).String()
	printCollectors = kingpin.Flag(
		"collectors.print",
		"If true, print available collectors and exit.",
	).Bool()

	flagSingleMode          = kingpin.Flag("single-mode", "run as single node").Default("1").Int()
	flagTeamID              = kingpin.Flag("team-id", "User ID").String()
	flagUploaderUID         = kingpin.Flag("uploader-uid", "uploader UID").String()
	flagAK                  = kingpin.Flag("ak", `Access Key`).String()
	flagSK                  = kingpin.Flag("sk", `Secret Key`).String()
	flagHost                = kingpin.Flag("host", `eg. ip addr`).String()
	flagPort                = kingpin.Flag("port", `listen port`).Default("9100").Int()
	flagEnableAllCollectors = kingpin.Flag("enabled", `enabled collectors`).Default("1ffffff").String()
	flagInstallPath         = kingpin.Flag("installpath", ``).String()

	//flagRemoteHost             = kingpin.Flag("remote-host", `data bridge addr`).Default("http://47.99.146.133:9527/").String()
	flagRemoteHost = kingpin.Flag("remote-host", `data bridge addr`).Default("http://172.16.0.12:10401/").String()

	flagRemoteMetricsWritePath = kingpin.Flag("metric-path", ``).Default("v1/write").String()
	flagRemoteEnvWritePath     = kingpin.Flag("env-path", ``).Default("v1/write/env").String()

	flagScrapeInterval        = kingpin.Flag("scrape-interval", "frequency to upload data").Default("3").Int()
	flagEnvInfoScrapeInterval = kingpin.Flag("scrape-env-interval", "frequency to upload env info").Default("10").Int()

	flagVersionInfo = kingpin.Flag("version", "show version info").Bool()

	flagProvider = kingpin.Flag("provider", "cloud service provider").Default("aliyun").String()
)

func checkArgs() error {

	exepath := os.Args[0]
	cloudcare.CorsairInstallPath = exepath

	cfg.Cfg.SingleMode = *flagSingleMode

	if *flagTeamID == "" || *flagUploaderUID == "" || *flagAK == "" || *flagSK == "" {
		log.Fatal("invalid argument")
	}

	cfg.Cfg.TeamID = *flagTeamID
	cfg.Cfg.UploaderUID = *flagUploaderUID
	cfg.Cfg.AK = *flagAK
	cfg.Cfg.SK = *flagSK // cfg.XorEncode(*flagSK)

	cloudcare.CorsairTeamID = *flagTeamID
	cloudcare.CorsairUploaderUID = *flagUploaderUID
	cloudcare.CorsairAK = *flagAK
	cloudcare.CorsairSK = cfg.Cfg.SK

	cfg.Cfg.Port = *flagPort
	cloudcare.CorsairPort = *flagPort

	if *flagHost != "" {
		cfg.Cfg.Host = *flagHost
	} else {
		cfg.Cfg.Host = "default"
	}
	cloudcare.CorsairHost = cfg.Cfg.Host

	cfg.Cfg.ScrapeInterval = *flagScrapeInterval
	cloudcare.CorsairScrapeInterval = *flagScrapeInterval

	cfg.Cfg.ScrapeEnvInterval = *flagEnvInfoScrapeInterval
	cloudcare.CorsairScrapeEnvInterval = *flagEnvInfoScrapeInterval

	cfg.Cfg.Provider = *flagProvider

	cfg.Cfg.RemoteHost = *flagRemoteHost
	cloudcare.CorsairRemoteHost = cfg.Cfg.RemoteHost
	cloudcare.CorsairMetricsWritePath = *flagRemoteMetricsWritePath
	cloudcare.CorsairEnvWritePath = *flagRemoteEnvWritePath

	setEnableCollectors(*flagEnableAllCollectors)

	cfg.Cfg.Collectors = make(map[string]bool)
	for _, v := range collectorItemList {
		cfg.Cfg.Collectors[v.name] = v.enabled
	}

	return nil
}

func main() {

	log.AddFlags(kingpin.CommandLine)
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

	checkArgs()

	if err := envinfo.InitOsquery(cloudcare.CorsairInstallPath); err != nil {
		log.Warnf("init osquery failed: %s", err)
	}

	enables := loadEnableCollectors()

	initWbem()

	isInteractive, err := svc.IsAnInteractiveSession()
	if err != nil {
		log.Fatal(err)
	}

	stopCh := make(chan bool)
	if !isInteractive {
		go svc.Run(serviceName, &wmiExporterService{stopCh: stopCh})
	}

	collectors, err := loadCollectors(enables)
	if err != nil {
		log.Fatalf("%s", err)
	}

	log.Infof("Enabled metric collectors: %v", strings.Join(keys(collectors), ", "))

	nodeCollector := WmiCollector{collectors: collectors}
	prometheus.MustRegister(nodeCollector)

	http.Handle(*metricsPath, promhttp.Handler())

	envRegister := prometheus.NewRegistry()
	envcCollector := envinfo.NewEnvCollector()
	envRegister.MustRegister(envcCollector)
	http.Handle(*envinfoPath, promhttp.HandlerFor(envRegister, promhttp.HandlerOpts{}))

	http.HandleFunc(*metaPath, func(w http.ResponseWriter, r *http.Request) {
		hostName, err := os.Hostname()
		if err != nil {
			//log.Printf("[error] %s, ignored", err.Error())
		}
		j, err := json.Marshal(&cfg.Meta{
			UploaderUID: cfg.Cfg.UploaderUID,
			Host:        cfg.Cfg.Host,
			HostName:    hostName,
			Provider:    cfg.Cfg.Provider,
		})
		if err != nil {
			log.Errorf("[error] %s, ignored", err.Error())
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

	//log.Infoln("Starting Corsair", version.Info())
	//log.Infoln("Build context", version.BuildContext())

	if cfg.Cfg.SingleMode == 1 {

		if err := cloudcare.Start(); err != nil {
			log.Fatal(err)
		}
	}

	go func() {
		log.Infoln("Starting server on", fmt.Sprintf("localhost:%d", *flagPort))
		log.Fatalf("cannot start Corsair: %s", http.ListenAndServe(fmt.Sprintf(":%d", *flagPort), nil))
	}()

	for {
		if <-stopCh {
			log.Infoln("Shutting down Corsair")
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
				log.Error(fmt.Sprintf("unexpected control request #%d", c))
			}
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
}
