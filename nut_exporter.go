package main

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
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

	infoFilter = kingpin.Flag(
		"nut.info_vars", "A comma-separated list of variable names to export as labels of the variable_info metric, typically string variables such as battery.mfr.date or ups.firmware. See the variable notes in README. ($NUT_EXPORTER_INFO_VARIABLES)",
	).Envar("NUT_EXPORTER_INFO_VARIABLES").Default("").String()

	onRegex = kingpin.Flag(
		"nut.on_regex", "This regular expression will be used to determine if the var's value should be coaxed to 1 if it is a string. Match is case-insensitive. ($NUT_EXPORTER_ON_REGEX)",
	).Envar("NUT_EXPORTER_ON_REGEX").Default("^(enable|enabled|on|true|active|activated)$").String()

	offRegex = kingpin.Flag(
		"nut.off_regex", "This regular expression will be used to determine if the var's value should be coaxed to 0 if it is a string. Match is case-insensitive. ($NUT_EXPORTER_OFF_REGEX)",
	).Envar("NUT_EXPORTER_OFF_REGEX").Default("^(disable|disabled|off|false|inactive|deactivated)$").String()

	statusList = kingpin.Flag(
		"nut.statuses", "A comma-separated list of statuses labels that will always be set by the exporter. If NUT does not set these flags, the exporter will force the network_ups_tools_ups_status{flag=\"NAME\"} to 0. See the ups.status notes in README.' ($NUT_EXPORTER_STATUSES)",
	).Envar("NUT_EXPORTER_STATUSES").Default("OL,OB,LB,HB,RB,CHRG,DISCHRG,BYPASS,CAL,OFF,OVER,TRIM,BOOST,FSD,SD").String()

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

type metricsHandler struct {
	mu       sync.Mutex
	handlers map[string]*http.Handler
}

// splitList splits a comma-separated list, trimming spaces and skipping empty items. Be nice and
// clear spaces for those that like them
func splitList(list string) []string {
	items := []string{}
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		items = append(items, item)
	}
	return items
}

// optsFromQuery applies the query string overrides to the command line options. An empty
// parameter, an empty list or a port out of 1-65535 keeps the command line value
func optsFromQuery(opts collectors.NutCollectorOpts, query url.Values) collectors.NutCollectorOpts {
	opts.Ups = query.Get("ups")

	if query.Get("server") != "" {
		opts.Server = query.Get("server")
	}

	if query.Get("serverport") != "" {
		if port, err := strconv.Atoi(query.Get("serverport")); err == nil && port > 0 && port <= 65535 {
			opts.ServerPort = port
		}
	}

	if query.Get("username") != "" {
		opts.Username = query.Get("username")
	}

	if query.Get("password") != "" {
		opts.Password = query.Get("password")
	}

	if list := splitList(query.Get("variables")); len(list) > 0 {
		opts.Variables = list
	}

	if list := splitList(query.Get("info_variables")); len(list) > 0 {
		opts.InfoVariables = list
	}

	if list := splitList(query.Get("statuses")); len(list) > 0 {
		opts.Statuses = list
	}

	return opts
}

// cacheKey identifies a handler by every option a request can override, so that requests with
// different options do not share a collector. Fields are quoted so that no value can collide with
// another, and the password is hashed so that the key does not hold it in clear
func cacheKey(opts collectors.NutCollectorOpts) string {
	return fmt.Sprintf("%q %d %q %q %x %q %q %q", opts.Server, opts.ServerPort, opts.Ups,
		opts.Username, sha256.Sum256([]byte(opts.Password)), opts.Variables, opts.InfoVariables, opts.Statuses)
}

// getHandler returns the handler of these options, creating it on first use. The collector is
// built without holding the lock: building it may connect to the NUT server, which must not
// block the requests of the other handlers
func (h *metricsHandler) getHandler(opts collectors.NutCollectorOpts) (http.Handler, error) {
	cacheName := fmt.Sprintf("%s:%d/%s", opts.Server, opts.ServerPort, opts.Ups)
	key := cacheKey(opts)

	h.mu.Lock()
	tmp, ok := h.handlers[key]
	h.mu.Unlock()
	if ok {
		logger.Debug(fmt.Sprintf("Using existing handler for UPS `%s`", cacheName))
		return *tmp, nil
	}

	//Build a custom registry to include only the UPS metrics on the UPS metrics path
	logger.Info(fmt.Sprintf("Creating new registry, handler, and collector for UPS `%s`", cacheName))
	registry := prometheus.NewRegistry()
	promHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry})
	promHandler = promhttp.InstrumentMetricHandler(registry, promHandler)

	nutCollector, err := collectors.NewNutCollector(opts, logger)
	if err != nil {
		return nil, err
	}
	registry.MustRegister(nutCollector)

	h.mu.Lock()
	defer h.mu.Unlock()
	// Another request may have created it meanwhile: keep the first one
	if tmp, ok := h.handlers[key]; ok {
		return *tmp, nil
	}
	h.handlers[key] = &promHandler
	return promHandler, nil
}

func (h *metricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	promHandler, err := h.getHandler(optsFromQuery(collectorOpts, r.URL.Query()))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - InternalServer Error"))
		logger.Error("Internal server error", "err", err)
		return
	}

	promHandler.ServeHTTP(w, r)
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

	variables := splitList(*enableFilter)
	hasUpsStatusVariable := false
	for _, variable := range variables {
		// Special handling because this is an important and commonly needed variable
		if variable == "ups.status" {
			hasUpsStatusVariable = true
		}
	}

	if !hasUpsStatusVariable {
		logger.Warn("Exporter has been started without `ups.status` variable to be exported with --nut.vars_enable. Online/offline/etc statuses will not be reported!")
	}

	infoVariables := splitList(*infoFilter)
	statuses := splitList(*statusList)

	collectorOpts = collectors.NutCollectorOpts{
		Namespace:         *metricsNamespace,
		Server:            *server,
		ServerPort:        *serverport,
		Username:          *nutUsername,
		Password:          nutPassword,
		DisableDeviceInfo: *disableDeviceInfo,
		Variables:         variables,
		InfoVariables:     infoVariables,
		Statuses:          statuses,
		OnRegex:           *onRegex,
		OffRegex:          *offRegex,
	}

	if *printMetrics {
		/* Make a channel and function to send output along */
		var out chan *prometheus.Desc
		done := make(chan struct{})
		eatOutput := func(in <-chan *prometheus.Desc) {
			defer close(done)
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
		/* Wait for the last item to be printed before exiting */
		<-done

		os.Exit(0)
	}

	logger.Info("Starting nut_exporter", "version", Version)

	handler := &metricsHandler{
		handlers: make(map[string]*http.Handler),
	}

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
