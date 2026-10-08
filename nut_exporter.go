package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/exporter-toolkit/web/kingpinflag"

	"github.com/DRuggeri/nut_exporter/v3/collectors"
)

var Version = "testing"

var (
	server = kingpin.Flag(
		"nut.server", "Hostname or IP address of the server to connect to. ($NUT_EXPORTER_SERVER)",
	).Envar("NUT_EXPORTER_SERVER").Default("127.0.0.1").String()

	serverport = kingpin.Flag(
		"nut.serverport", "Port on the NUT server to connect to. ($NUT_EXPORTER_SERVERPORT)",
	).Envar("NUT_EXPORTER_SERVERPORT").Default("3493").Int()

	nutUsername = kingpin.Flag(
		"nut.username", "If set, will authenticate with this username to the server. Password must be set in NUT_EXPORTER_PASSWORD environment variable. ($NUT_EXPORTER_USERNAME)",
	).Envar("NUT_EXPORTER_USERNAME").String()
	nutPassword = ""

	disableDeviceInfo = kingpin.Flag(
		"nut.disable_device_info", "A flag to disable the generation of the device_info meta metric. ($NUT_EXPORTER_DISABLE_DEVICE_INFO)",
	).Envar("NUT_EXPORTER_DISABLE_DEVICE_INFO").Default("false").Bool()

	enableFilter = kingpin.Flag(
		"nut.vars_enable", "A comma-separated list of variable names to monitor. See the variable notes in README. ($NUT_EXPORTER_VARIABLES)",
	).Envar("NUT_EXPORTER_VARIABLES").Default("battery.charge,battery.voltage,battery.voltage.nominal,input.voltage,input.voltage.nominal,ups.load,ups.status").String()

	onRegex = kingpin.Flag(
		"nut.on_regex", "This regular expression will be used to determine if the var's value should be coaxed to 1 if it is a string. Match is case-insensitive. ($NUT_EXPORTER_ON_REGEX)",
	).Envar("NUT_EXPORTER_ON_REGEX").Default("^(enable|enabled|on|true|active|activated)$").String()

	offRegex = kingpin.Flag(
		"nut.off_regex", "This regular expression will be used to determine if the var's value should be coaxed to 0 if it is a string. Match is case-insensitive. ($NUT_EXPORTER_OFF_REGEX)",
	).Envar("NUT_EXPORTER_OFF_REGEX").Default("^(disable|disabled|off|false|inactive|deactivated)$").String()

	statusList = kingpin.Flag(
		"nut.statuses", "A comma-separated list of statuses labels that will always be set by the exporter. If NUT does not set these flags, the exporter will force the network_ups_tools_ups_status{flag=\"NAME\"} to 0. See the ups.status notes in README.' ($NUT_EXPORTER_STATUSES)",
	).Envar("NUT_EXPORTER_STATUSES").Default("OL,OB,LB,HB,RB,CHRG,DISCHRG,BYPASS,CAL,OFF,OVER,TRIM,BOOST,FSD,SD").String()

	scrapeTimeout = kingpin.Flag(
		"nut.scrape_timeout", "Overall budget for a single NUT scrape, covering dialing, the handshake, all I/O, and cleanup. Keep the Prometheus scrape_timeout larger than this value. ($NUT_EXPORTER_SCRAPE_TIMEOUT)",
	).Envar("NUT_EXPORTER_SCRAPE_TIMEOUT").Default("10s").Duration()

	metricsNamespace = kingpin.Flag(
		"metrics.namespace", "Metrics Namespace ($NUT_EXPORTER_METRICS_NAMESPACE)",
	).Envar("NUT_EXPORTER_METRICS_NAMESPACE").Default("network_ups_tools").String()

	tookitFlags = kingpinflag.AddFlags(kingpin.CommandLine, ":9199")

	metricsPath = kingpin.Flag(
		"web.telemetry-path", "Path under which to expose the UPS Prometheus metrics ($NUT_EXPORTER_WEB_TELEMETRY_PATH)",
	).Envar("NUT_EXPORTER_WEB_TELEMETRY_PATH").Default("/ups_metrics").String()

	exporterMetricsPath = kingpin.Flag(
		"web.exporter-telemetry-path", "Path under which to expose process metrics about this exporter ($NUT_EXPORTER_WEB_EXPORTER_TELEMETRY_PATH)",
	).Envar("NUT_EXPORTER_WEB_EXPORTER_TELEMETRY_PATH").Default("/metrics").String()

	printMetrics = kingpin.Flag(
		"printMetrics", "Print the metrics this exporter exposes and exits. Default: false ($NUT_EXPORTER_PRINT_METRICS)",
	).Envar("NUT_EXPORTER_PRINT_METRICS").Default("false").Bool()

	logLevel = kingpin.Flag(
		"log.level", "Minimum log level for messages. One of error, warn, info, or debug. Default: info ($NETGEAR_EXPORTER_LOG_LEVEL)",
	).Envar("NUT_EXPORTER__LOG_LEVEL").Default("info").String()

	logJson = kingpin.Flag(
		"log.json", "Format log lines as JSON. Default: false ($NETGEAR_EXPORTER_LOG_JSON)",
	).Envar("NUT_EXPORTER__LOG_JSON").Bool()
)
var collectorOpts collectors.NutCollectorOpts

var logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func init() {
	prometheus.MustRegister(promcollectors.NewBuildInfoCollector())
}

// instrumentationRegistry keeps the promhttp_metric_handler_* counters
// cumulative across scrapes. Per-target registries are created per request, so
// instrumenting those would reset the counters on every scrape.
var instrumentationRegistry = prometheus.NewRegistry()

// reRegistering wraps a Registerer so repeated instrumentation of freshly
// built handlers reuses the already registered metric families instead of
// panicking on duplicate registration.
type reRegistering struct {
	prometheus.Registerer
}

func (r reRegistering) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := r.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}
}

type metricsHandler struct {
	opts         collectors.NutCollectorOpts
	timeout      time.Duration
	instrumented http.Handler
}

func newMetricsHandler(opts collectors.NutCollectorOpts, timeout time.Duration) *metricsHandler {
	h := &metricsHandler{opts: opts, timeout: timeout}
	h.instrumented = promhttp.InstrumentMetricHandler(reRegistering{instrumentationRegistry}, http.HandlerFunc(h.serveScrape))
	return h
}

func (h *metricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.instrumented.ServeHTTP(w, r)
}

func (h *metricsHandler) serveScrape(w http.ResponseWriter, r *http.Request) {
	thisCollectorOpts := h.opts
	thisCollectorOpts.Ups = r.URL.Query().Get("ups")

	if r.URL.Query().Get("server") != "" {
		thisCollectorOpts.Server = r.URL.Query().Get("server")
	}

	if r.URL.Query().Get("serverport") != "" {
		port, err := strconv.Atoi(r.URL.Query().Get("serverport"))
		if err != nil || port < 1 || port > 65535 {
			http.Error(w, fmt.Sprintf("invalid serverport %q", r.URL.Query().Get("serverport")), http.StatusBadRequest)
			return
		}
		thisCollectorOpts.ServerPort = port
	}

	if r.URL.Query().Get("username") != "" {
		thisCollectorOpts.Username = r.URL.Query().Get("username")
	}

	if r.URL.Query().Get("password") != "" {
		thisCollectorOpts.Password = r.URL.Query().Get("password")
	}

	if r.URL.Query().Get("variables") != "" {
		thisCollectorOpts.Variables = strings.Split(r.URL.Query().Get("variables"), ",")
	}

	if r.URL.Query().Get("statuses") != "" {
		thisCollectorOpts.Statuses = strings.Split(r.URL.Query().Get("statuses"), ",")
	}

	// The scrape is bounded by the caller: the request context (cancellation
	// included) and the configured overall budget. Every collector, registry,
	// and connection used for this scrape is owned by this request, so
	// credentials and filters can never leak into another scrape's config.
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	thisCollectorOpts.Context = ctx

	registry := prometheus.NewRegistry()
	nutCollector, err := collectors.NewNutCollector(thisCollectorOpts, logger)
	if err != nil {
		http.Error(w, "500 - InternalServer Error", http.StatusInternalServerError)
		logger.Error("Internal server error", "err", err)
		return
	}
	registry.MustRegister(nutCollector)

	// Never answer a scrape whose budget expired mid-flight with a 200
	// carrying a partially gathered metric set.
	gatherer := prometheus.GathererFunc(func() ([]*dto.MetricFamily, error) {
		metrics, err := registry.Gather()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return nil, context.DeadlineExceeded
		}
		return metrics, err
	})

	promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}).ServeHTTP(w, r.WithContext(ctx))
}

func main() {
	//flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.Version(Version)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	/* Reconfigure logger after parsing arguments */
	opts := &slog.HandlerOptions{}
	switch *logLevel {
	case "error":
		opts.Level = slog.LevelError
	case "warn":
		opts.Level = slog.LevelWarn
	case "info":
		opts.Level = slog.LevelInfo
	case "debug":
		opts.Level = slog.LevelDebug
	}
	if *logJson {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, opts))
		slog.SetDefault(logger)
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stderr, opts))
		slog.SetDefault(logger)
	}

	if *nutUsername != "" {
		logger.Debug("Authenticating to NUT server")
		nutPassword = os.Getenv("NUT_EXPORTER_PASSWORD")
		if nutPassword == "" {
			logger.Error("Username set, but NUT_EXPORTER_PASSWORD environment variable missing. Cannot authenticate!")
			os.Exit(2)
		}
	}

	variables := []string{}
	hasUpsStatusVariable := false
	for _, varName := range strings.Split(*enableFilter, ",") {
		// Be nice and clear spaces for those that like them
		variable := strings.Trim(varName, " ")
		if variable == "" {
			continue
		}
		variables = append(variables, variable)

		// Special handling because this is an important and commonly needed variable
		if variable == "ups.status" {
			hasUpsStatusVariable = true
		}
	}

	if !hasUpsStatusVariable {
		logger.Warn("Exporter has been started without `ups.status` variable to be exported with --nut.vars_enable. Online/offline/etc statuses will not be reported!")
	}

	statuses := []string{}
	for _, status := range strings.Split(*statusList, ",") {
		// Be nice and clear spaces for those that like them
		stat := strings.Trim(status, " ")
		if stat == "" {
			continue
		}
		statuses = append(statuses, strings.Trim(stat, " "))
	}

	collectorOpts = collectors.NutCollectorOpts{
		Namespace:         *metricsNamespace,
		Server:            *server,
		ServerPort:        *serverport,
		Username:          *nutUsername,
		Password:          nutPassword,
		DisableDeviceInfo: *disableDeviceInfo,
		Variables:         variables,
		Statuses:          statuses,
		OnRegex:           *onRegex,
		OffRegex:          *offRegex,
	}

	if *printMetrics {
		/* Make a channel and function to send output along */
		var out chan *prometheus.Desc
		eatOutput := func(in <-chan *prometheus.Desc) {
			for desc := range in {
				/* Weaksauce... no direct access to the variables */
				//Desc{fqName: "the_name", help: "help text", constLabels: {}, variableLabels: []}
				tmp := desc.String()
				vals := strings.Split(tmp, `"`)
				fmt.Printf("  %s - %s\n", vals[1], vals[3])
			}
		}

		/* Interesting juggle here...
		   - Make a channel the describe function can send output to
		   - Start the printing function that consumes the output in the background
		   - Call the describe function to feed the channel (which blocks until the consume function eats a message)
		   - When the describe function exits after returning the last item, close the channel to end the background consume function
		*/
		fmt.Println("NUT")
		nutCollector, _ := collectors.NewNutCollector(collectorOpts, logger)
		out = make(chan *prometheus.Desc)
		go eatOutput(out)
		nutCollector.Describe(out)
		close(out)

		os.Exit(0)
	}

	logger.Info("Starting nut_exporter", "version", Version)

	if *scrapeTimeout <= 0 {
		logger.Error("Invalid scrape timeout - must be greater than zero", "timeout", *scrapeTimeout)
		os.Exit(2)
	}
	handler := newMetricsHandler(collectorOpts, *scrapeTimeout)

	http.Handle(*metricsPath, handler)
	http.Handle(*exporterMetricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>NUT Exporter</title></head>
             <body>
             <h1>NUT Exporter</h1>
             <p><a href='` + *metricsPath + `'>UPS metrics</a></p>
             <p><a href='` + *exporterMetricsPath + `'>Exporter metrics</a></p>
             </body>
             </html>`))
	})

	srv := &http.Server{}
	if err := web.ListenAndServe(srv, tookitFlags, logger); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
