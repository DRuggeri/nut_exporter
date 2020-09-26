package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/DRuggeri/nut_exporter/collectors"
)

var (
	server = kingpin.Flag(
		"nut.server", "Hostname or IP address of the server to connect to.' ($NUT_EXPORTER_SERVER)",
	).Envar("NUT_EXPORTER_SERVER").Default("127.0.0.1").String()

	nutUsername = kingpin.Flag(
		"nut.username", "If set, will authenticate with this username to the server. Password must be set in NUT_EXPORTER_PASSWORD environment variable.' ($NUT_EXPORTER_USERNAME)",
	).Envar("NUT_EXPORTER_SERVER").String()
	nutPassword = ""

	enableFilter = kingpin.Flag(
		"nut.vars_enable", "A comma-separated list of variable names to monitor. See the variable notes in README.' ($NUT_EXPORTER_VARIABLES)",
	).Envar("NUT_EXPORTER_VARIABLES").Default("battery.charge,battery.voltage,battery.voltage.nominal,input.voltage,input.voltage.nominal,ups.load").String()

	metricsNamespace = kingpin.Flag(
		"metrics.namespace", "Metrics Namespace ($NUT_EXPORTER_METRICS_NAMESPACE)",
	).Envar("NUT_EXPORTER_METRICS_NAMESPACE").Default("network_ups_tools").String()

	listenAddress = kingpin.Flag(
		"web.listen-address", "Address to listen on for web interface and telemetry ($NUT_EXPORTER_WEB_LISTEN_ADDRESS)",
	).Envar("NUT_EXPORTER_WEB_LISTEN_ADDRESS").Default(":9199").String()

	metricsPath = kingpin.Flag(
		"web.telemetry-path", "Path under which to expose Prometheus metrics ($NUT_EXPORTER_WEB_TELEMETRY_PATH)",
	).Envar("NUT_EXPORTER_WEB_TELEMETRY_PATH").Default("/metrics").String()

	exporterMetricsPath = kingpin.Flag(
		"web.exporter-telemetry-path", "Path under which to expose process metrics about this exporter ($NUT_EXPORTER_WEB_EXPORTER_TELEMETRY_PATH)",
	).Envar("NUT_EXPORTER_WEB_EXPORTER_TELEMETRY_PATH").Default("/exporter_metrics").String()


	authUsername = kingpin.Flag(
		"web.auth.username", "Username for web interface basic auth ($NUT_EXPORTER_WEB_AUTH_USERNAME)",
	).Envar("NUT_EXPORTER_WEB_AUTH_USERNAME").String()
	authPassword = ""

	tlsCertFile = kingpin.Flag(
		"web.tls.cert_file", "Path to a file that contains the TLS certificate (PEM format). If the certificate is signed by a certificate authority, the file should be the concatenation of the server's certificate, any intermediates, and the CA's certificate ($NUT_EXPORTER_WEB_TLS_CERTFILE)",
	).Envar("NUT_EXPORTER_WEB_TLS_KEYFILE").ExistingFile()

	tlsKeyFile = kingpin.Flag(
		"web.tls.key_file", "Path to a file that contains the TLS private key (PEM format) ($NUT_EXPORTER_WEB_TLS_KEYFILE)",
	).Envar("NUT_EXPORTER_WEB_TLS_KEYFILE").ExistingFile()

	printMetrics = kingpin.Flag(
		"printMetrics", "Print the metrics this exporter exposes and exits. Default: false ($NUT_EXPORTER_PRINT_METRICS)",
	).Envar("NUT_EXPORTER_PRINT_METRICS").Default("false").Bool()
)
var collectorOpts collectors.NutCollectorOpts

func init() {
	prometheus.MustRegister(version.NewCollector(*metricsNamespace))
}

type basicAuthHandler struct {
	handler  http.HandlerFunc
	username string
	password string
}

type metricsHandler struct {
}

func (h *basicAuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok || username != h.username || password != h.password {
		log.Errorf("Invalid HTTP auth from `%s`", r.RemoteAddr)
		w.Header().Set("WWW-Authenticate", "Basic realm=\"metrics\"")
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}
	h.handler(w, r)
	return
}

func (h *metricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	thisCollectorOpts := collectors.NutCollectorOpts{
                Namespace: collectorOpts.Namespace,
                Server:    collectorOpts.Server,
                Username:  collectorOpts.Username,
                Password:  collectorOpts.Password,
                Variables: collectorOpts.Variables,
		Ups:       r.URL.Query().Get("ups"),
        }

	nutCollector, err := collectors.NewNutCollector(thisCollectorOpts)
        if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - InternalServer Error"))
                log.Error(err)
		return
        }
	registry := prometheus.NewRegistry()
	registry.MustRegister(nutCollector)

	newHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	newHandler = promhttp.InstrumentMetricHandler(registry, newHandler)
	newHandler.ServeHTTP(w,r)
	return
}

func basicAuthHandlerBuilder(parentHandler http.Handler) http.Handler {
	if *authUsername != "" && authPassword != "" {
		return &basicAuthHandler{
			handler:  parentHandler.ServeHTTP,
			username: *authUsername,
			password: authPassword,
		}
	}

	return parentHandler
}

func main() {
	log.AddFlags(kingpin.CommandLine)
	kingpin.Version(version.Print("nut_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	if *nutUsername != "" {
		log.Infoln("Authenticating to NUT server")
		nutPassword = os.Getenv("NUT_EXPORTER_PASSWORD")
		if nutPassword == "" {
			log.Error("Username set, but NUT_EXPORTER_PASSWORD environment variable missing. Cannot authenticate!")
			os.Exit(2)
		}
	}

	variables := []string{}
	for _, varName := range strings.Split(*enableFilter, ",") {
		// Be nice and clear spaces for those that like them
		variables = append(variables, strings.Trim(varName, " "))
	}

	collectorOpts = collectors.NutCollectorOpts{
		Namespace: *metricsNamespace,
		Server:    *server,
		Username:  *nutUsername,
		Password:  nutPassword,
		Variables: variables,
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
		nutCollector, _ := collectors.NewNutCollector(collectorOpts)
		out = make(chan *prometheus.Desc)
		go eatOutput(out)
		nutCollector.Describe(out)
		close(out)

		os.Exit(0)
	}

	log.Infoln("Starting nut_exporter", version.Info())
	log.Infoln("Build context", version.BuildContext())

	authPassword = os.Getenv("NUT_EXPORTER_WEB_AUTH_PASSWORD")
	http.Handle(*metricsPath, basicAuthHandlerBuilder(&metricsHandler{}))
	http.Handle(*exporterMetricsPath, basicAuthHandlerBuilder(promhttp.Handler()))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>NUT Exporter</title></head>
             <body>
             <h1>NUT Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})

	if *tlsCertFile != "" && *tlsKeyFile != "" {
		log.Infoln("Listening TLS on", *listenAddress)
		log.Fatal(http.ListenAndServeTLS(*listenAddress, *tlsCertFile, *tlsKeyFile, nil))
	} else {
		log.Infoln("Listening on", *listenAddress)
		log.Fatal(http.ListenAndServe(*listenAddress, nil))
	}
}
