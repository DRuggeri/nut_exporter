package collectors

import (
	"time"
	"strings"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/robbiet480/go.nut"
)

var deviceLabels = []string{ "model", "mfr", "serial", "type", "description", "contact", "location", "part", "macaddr", "uptime" }

type NutCollector struct {
	deviceMetric *prometheus.GaugeVec
	varsMetric *prometheus.GaugeVec
	opts	NutCollectorOpts

	scrapesTotalMetric              prometheus.Counter
	scrapeErrorsTotalMetric         prometheus.Counter
	lastScrapeErrorMetric           prometheus.Gauge
	lastScrapeTimestampMetric       prometheus.Gauge
	lastScrapeDurationSecondsMetric prometheus.Gauge
}

type NutCollectorOpts struct {
	Namespace       string
	Server	string
	Ups	string
	Username	string
	Password	string
	Variables	[]string
}

func NewNutCollector(opts NutCollectorOpts) (*NutCollector, error) {
	namespace := opts.Namespace
	subsystem := "ups"

	deviceMetric := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "device",
			Help:      "UPS device information",
			},
			append([]string{"ups"}, deviceLabels...),
		)

	varsMetric := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "variable",
			Help:      "Variable from Network UPS Tools",
			},
			[]string{"ups", "variable"},
		)

	scrapesTotalMetric := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "scrapes_total",
			Help:      "Total number of scrapes for network UPS tools variables",
		},
	)

	scrapeErrorsTotalMetric := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "scrape_errors_total",
			Help:      "Total number of scrapes errors for Network UPS Tools variables",
		},
	)

	lastScrapeErrorMetric := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "last_scrape_error",
			Help:      "Whether the last scrape of Network UPS Tools variables resulted in an error (1 for error, 0 for success)",
		},
	)

	lastScrapeTimestampMetric := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "last_scrape_timestamp",
			Help:      "Number of seconds since 1970 since last scrape of Network UPS Tools variables",
		},
	)

	lastScrapeDurationSecondsMetric := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "last_scrape_duration_seconds",
			Help:      "Duration of the last scrape of Network UPS Tools variables",
		},
	)

	return &NutCollector{
		deviceMetric: deviceMetric,
		varsMetric: varsMetric,
		opts: opts,

		scrapesTotalMetric:              scrapesTotalMetric,
		scrapeErrorsTotalMetric:         scrapeErrorsTotalMetric,
		lastScrapeErrorMetric:           lastScrapeErrorMetric,
		lastScrapeTimestampMetric:       lastScrapeTimestampMetric,
		lastScrapeDurationSecondsMetric: lastScrapeDurationSecondsMetric,
	}, nil
}

func (c *NutCollector) Collect(ch chan<- prometheus.Metric) {
	var begun = time.Now()

	errorMetric := float64(0)

	client, err := nut.Connect(c.opts.Server)
	if err == nil {
		log.Debugf("Connected to server `%v`", c.opts.Server)
		if c.opts.Username != "" && c.opts.Password != "" {
			_, err = client.Authenticate(c.opts.Username, c.opts.Password)
			if err != nil {
				log.Debugf("Authenticated to `%v` as user `%v", c.opts.Server, c.opts.Username)
			}
		}
	}

        if err != nil {
		log.Error(err)
		c.scrapeErrorsTotalMetric.Inc()
		errorMetric = float64(1)
	} else {
		var upsList []nut.UPS
		if c.opts.Ups != "" {
			ups, err := nut.NewUPS(c.opts.Ups, &client)
			if err == nil {
				log.Debugf("Instantiated UPS named `%s`", c.opts.Ups)
				upsList = append(upsList, ups)
			} else {
				log.Errorf("Failure instantiating the UPS named `%s`: %v", c.opts.Ups, err)
			}
		} else {
			tmp, err := client.GetUPSList()
			if err == nil {
				log.Debugf("Obtained list of UPS devices")
				upsList = tmp
			} else {
				log.Errorf("Failure getting the list of UPS devices: %v", err)
			}
		}



		for _, ups := range upsList {
			device := make(map[string]string)
			for _, label := range deviceLabels {
				device[label] = "unset"
			}

			log.Debugf("UPS info:")
			log.Debugf("  Name: %v", ups.Name)
			log.Debugf("  Description: %v", ups.Description)
			log.Debugf("  Master: %v", ups.Master)
			log.Debugf("  NumberOfLogins: %v", ups.NumberOfLogins)
			log.Debug ("  Clients:")
			for i, clientName := range ups.Clients {
				log.Debugf("    %v: %v", i, clientName)
			}
			log.Debug ("  Commands:")
			for _, command := range ups.Commands {
				log.Debugf("    %v: %v", command.Name, command.Description)
			}
			log.Debug ("  Variables:")
			for _, variable := range ups.Variables {
				log.Debugf("    %v:", variable.Name)
				log.Debugf("      Value: '%v'", variable.Value)
				log.Debugf("      Type: %v", variable.Type)
				log.Debugf("      Description: '%v'", variable.Description)
				log.Debugf("      Writeable: %v", variable.Writeable)
				log.Debugf("      MaximumLength: %v", variable.MaximumLength)
				log.Debugf("      OriginalType: %v", variable.OriginalType)

				path := strings.Split(variable.Name, ".")
				if path[0] == "device" {
					device[path[1]] = fmt.Sprintf("%v", variable.Value)
				}

				/* Done special processing - now get as general as possible and gather all requested or number-like metrics */
				if len(c.opts.Variables) == 0 || sliceContains(c.opts.Variables, variable.Name) {
					log.Debugf("      Export the variable? true")
					value := float64(0)

					/* All numbers are coaxed to native types by the library, so at this point we know
					   we cannot set this value because a string will never be a float-like number */
					if variable.Type == "STRING" {
						continue
					}

					/* This is overkill - the library only deals with bool, string, int64 and float64 */
					switch v := variable.Value.(type) {
					case bool:
						if v {
							value = float64(1)
						}
					case int:
						value = float64(v)
					case int8:
						value = float64(v)
					case int16:
						value = float64(v)
					case int64:
						value = float64(v)
					case float32:
						value = float64(v)
					case float64:
						value = float64(v)
					default:
						log.Warnf("Variable `%s` is of unknown type `%s`")
						continue
					}

					c.varsMetric.WithLabelValues(ups.Name, variable.Name).Set(value)
				} else {
					log.Debugf("      Export the variable? false")
				}
			}

			deviceValues := []string{ups.Name}
			for _, label := range deviceLabels {
				deviceValues = append(deviceValues, device[label])
			}
			c.deviceMetric.WithLabelValues(deviceValues...).Set(1)
		}
		client.Disconnect()
	}
	c.deviceMetric.Collect(ch)
	c.varsMetric.Collect(ch)

	c.scrapeErrorsTotalMetric.Collect(ch)

	c.scrapesTotalMetric.Inc()
	c.scrapesTotalMetric.Collect(ch)

	c.lastScrapeErrorMetric.Set(errorMetric)
	c.lastScrapeErrorMetric.Collect(ch)

	c.lastScrapeTimestampMetric.Set(float64(time.Now().Unix()))
	c.lastScrapeTimestampMetric.Collect(ch)

	c.lastScrapeDurationSecondsMetric.Set(time.Since(begun).Seconds())
	c.lastScrapeDurationSecondsMetric.Collect(ch)
}

func (c *NutCollector) Describe(ch chan<- *prometheus.Desc) {
	c.deviceMetric.Describe(ch)
	c.varsMetric.Describe(ch)
	c.scrapesTotalMetric.Describe(ch)
	c.scrapeErrorsTotalMetric.Describe(ch)
	c.lastScrapeErrorMetric.Describe(ch)
	c.lastScrapeTimestampMetric.Describe(ch)
	c.lastScrapeDurationSecondsMetric.Describe(ch)
}

func sliceContains(c []string, value string) bool {
	for _, sliceValue := range c {
		if sliceValue == value {
			return true
		}
	}
	return false
}
