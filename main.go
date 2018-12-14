// +build windows

package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"

	"wmi_exporter/cfg"
	"wmi_exporter/cloudcare"
	"wmi_exporter/collector"
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

func listAllCollectors() map[string]bool {
	m := make(map[string]bool)
	defs := strings.Split(defaultCollectors, ",")
	bhave := false
	for k, _ := range collector.Factories {
		if cfg.Cfg.EnableAll > 0 {
			m[k] = true
		} else {
			bhave = false
			for _, v := range defs {
				if v == k {
					bhave = true
					break
				}
			}
			m[k] = bhave
		}
	}
	return m
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
	//enabled := expandEnabledCollectors(list)

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
	flagSingleMode          = kingpin.Flag("single-mode", "run as single node").Default("0").Int()
	flagInit                = kingpin.Flag("init", `init collector`).Bool()
	flagUpgrade             = kingpin.Flag("upgrade", ``).Bool()
	flagHost                = kingpin.Flag("host", `eg. ip addr`).String()
	flagRemoteHost          = kingpin.Flag("remote-host", `data bridge addr`).Default("http://kodo.cloudcare.com/v1/write").String()
	flagScrapeInterval      = kingpin.Flag("scrape-interval", "frequency to upload data").Default("15").Int()
	flagTeamID              = kingpin.Flag("team-id", "User ID").String()
	flagCloudAssetID        = kingpin.Flag("cloud-asset-id", "cloud instance ID").String()
	flagAK                  = kingpin.Flag("ak", `Access Key`).String()
	flagSK                  = kingpin.Flag("sk", `Secret Key`).String()
	flagPort                = kingpin.Flag("port", `web listen port`).Default("9100").Int()
	flagCfgFile             = kingpin.Flag("cfg", `configure file`).Default("cfg.yml").String()
	flagVersionInfo         = kingpin.Flag("version", "show version info").Bool()
	flagEnableAllCollectors = kingpin.Flag("enable-all", "enable all collectors").Default("0").Int()
)

func initCfg(cfgpath string) error {

	cfg.Cfg.SingleMode = *flagSingleMode

	if *flagHost != "" {
		cfg.Cfg.Host = *flagHost
	}

	cfg.Cfg.RemoteHost = *flagRemoteHost
	cfg.Cfg.ScrapeInterval = *flagScrapeInterval
	cfg.Cfg.EnableAll = *flagEnableAllCollectors

	// unique-id 为必填参数
	if *flagTeamID == "" {
		log.Fatal("invalid unique-id")
	} else {
		cfg.Cfg.TeamID = *flagTeamID
	}

	if *flagCloudAssetID == "" {
		log.Fatal("invalid cloud assert id")
	} else {
		cfg.Cfg.CloudAssetID = *flagCloudAssetID
	}

	if *flagAK == "" {
		log.Fatal("invalid ak")
	} else {
		cfg.Cfg.AK = *flagAK
	}

	if *flagSK == "" {
		log.Fatal("invalid sk")
	} else {
		cfg.Cfg.SK = cfg.XorEncode(*flagSK)
	}

	cfg.Cfg.Port = *flagPort

	cfg.Cfg.Collectors = listAllCollectors()

	if cfg.Cfg.EnableAll == 1 {
		for k, _ := range cfg.Cfg.Collectors {
			cfg.Cfg.Collectors[k] = true
		}
	}

	return cfg.DumpConfig(cfgpath)
}

func main() {
	var (
		// listenAddress = kingpin.Flag(
		// 	"telemetry.addr",
		// 	"host:port for WMI exporter.",
		// ).Default(":9182").String()
		metricsPath = kingpin.Flag(
			"telemetry.path",
			"URL path for surfacing collected metrics.",
		).Default("/metrics").String()
		// enabledCollectors = kingpin.Flag(
		// 	"collectors.enabled",
		// 	"Comma-separated list of collectors to use. Use '[defaults]' as a placeholder for all the collectors enabled by default.").
		// 	Default(filterAvailableCollectors(defaultCollectors)).String()
		printCollectors = kingpin.Flag(
			"collectors.print",
			"If true, print available collectors and exit.",
		).Bool()
	)

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

	execdir := path.Dir(os.Args[0])
	execdir, _ = filepath.Abs(execdir)
	cfgpath := fmt.Sprintf("%s\\%s", execdir, *flagCfgFile)
	_, err := os.Stat(cfgpath)
	if err != nil {
		_ = initCfg(cfgpath)
	}

	// if *flagInit {
	// 	_ = initCfg()
	// 	return
	// } else if *flagUpgrade {
	// 	// TODO
	// 	return
	// }

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

	if err := cfg.LoadConfig(cfgpath); err != nil {
		log.Fatalln("fail to load config file:", err)
	}
	cfg.DumpConfig(cfgpath) // load 过程中可能会修改 cfg.Cfg, 需重新写入

	enables := loadEnableCollectors()

	if cfg.Cfg.SingleMode == 1 {
		var scu *url.URL

		if err := cloudcare.Start(cfg.Cfg.RemoteHost, ""); err != nil {
			panic(err)
		}
		if scu != nil {
			time.Sleep(60 * 60 * time.Second)
			return
		}
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

	collectors, err := loadCollectors(enables)
	if err != nil {
		log.Fatalf("Couldn't load collectors: %s", err)
	}

	log.Infof("Enabled collectors: %v", strings.Join(keys(collectors), ", "))

	nodeCollector := WmiCollector{collectors: collectors}
	prometheus.MustRegister(nodeCollector)

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/health", healthCheck)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, *metricsPath, http.StatusMovedPermanently)
	})

	log.Infoln("Starting Corsair", version.Info())
	log.Infoln("Build context", version.BuildContext())

	go func() {
		log.Infoln("Starting server on", fmt.Sprintf("localhost:%d", *flagPort))
		log.Fatalf("cannot start Corsair: %s", http.ListenAndServe(fmt.Sprintf(":%d", *flagPort), nil))
	}()

	for {
		if <-stopCh {
			log.Info("Shutting down Corsair")
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
