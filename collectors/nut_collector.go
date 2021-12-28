package collectors

import (
	"fmt"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/robbiet480/go.nut"
)

var deviceLabels = []string{"model", "mfr", "serial", "type", "description", "contact", "location", "part", "macaddr"}

type NutCollector struct {
	deviceDesc *prometheus.Desc
	logger     log.Logger
	opts       NutCollectorOpts
}

type NutCollectorOpts struct {
	Namespace string
	Server    string
	Ups       string
	Username  string
	Password  string
	Variables []string
	Statuses  []string
}

func NewNutCollector(opts NutCollectorOpts, logger log.Logger) (*NutCollector, error) {
	deviceDesc := prometheus.NewDesc(prometheus.BuildFQName(opts.Namespace, "", "device_info"),
		"UPS Device information",
		deviceLabels, nil,
	)

	return &NutCollector{
		deviceDesc: deviceDesc,
		logger:     logger,
		opts:       opts,
	}, nil
}

func (c *NutCollector) Collect(ch chan<- prometheus.Metric) {
	level.Debug(c.logger).Log("msg", "Connecting to server", "server", c.opts.Server)
	client, err := nut.Connect(c.opts.Server)
	if err == nil {
		level.Debug(c.logger).Log("msg", "Connected to server", "server", c.opts.Server)
		if c.opts.Username != "" && c.opts.Password != "" {
			_, err = client.Authenticate(c.opts.Username, c.opts.Password)
			if err != nil {
				level.Debug(c.logger).Log("msg", "Authenticated", "server", c.opts.Server, "user", c.opts.Username)
			}
		}
	}

	if err != nil {
		level.Error(c.logger).Log("err", err)
	} else {
		upsList := []nut.UPS{}
		if c.opts.Ups != "" {
			ups, err := nut.NewUPS(c.opts.Ups, &client)
			if err == nil {
				level.Debug(c.logger).Log("msg", "Instantiated UPS", "name", c.opts.Ups)
				upsList = append(upsList, ups)
			} else {
				level.Error(c.logger).Log("msg", "Failure instantiating the UPS", "name", c.opts.Ups, "err", err)
			}
		} else {
			tmp, err := client.GetUPSList()
			if err == nil {
				level.Debug(c.logger).Log("msg", "Obtained list of UPS devices")
				upsList = tmp
			} else {
				level.Error(c.logger).Log("msg", "Failure getting the list of UPS devices", "err", err)
			}
		}

		if err != nil {
			ch <- prometheus.NewInvalidMetric(
				prometheus.NewDesc(prometheus.BuildFQName(c.opts.Namespace, "", "error"),
					"Failure gathering UPS variables", nil, nil),
				err)
		}

		if len(upsList) > 1 {
			level.Error(c.logger).Log("msg", "Multiple UPS devices were found by NUT for this scrap. For this configuration, you MUST scrape this exporter with a query string parameter indicating which UPS to scrape. Valid values of ups are:")
			for _, ups := range upsList {
				level.Error(c.logger).Log("name", ups.Name)
			}
			ch <- prometheus.NewInvalidMetric(
				prometheus.NewDesc(prometheus.BuildFQName(c.opts.Namespace, "", "error"),
					"Multiple UPS devices were found fron NUT. Please add a ups=<name> query string", nil, nil),
				err)
		}

		for _, ups := range upsList {
			device := make(map[string]string)
			for _, label := range deviceLabels {
				device[label] = ""
			}

			level.Debug(c.logger).Log(
				"msg", "UPS info",
				"name", ups.Name,
				"description", ups.Description,
				"master", ups.Master,
				"nmumber_of_logins", ups.NumberOfLogins,
			)
			for i, clientName := range ups.Clients {
				level.Debug(c.logger).Log("client", i, "name", clientName)
			}
			for _, command := range ups.Commands {
				level.Debug(c.logger).Log("command", command.Name, "description", command.Description)
			}
			for _, variable := range ups.Variables {
				level.Debug(c.logger).Log(
					"variable_name", variable.Name,
					"value", variable.Value,
					"type", variable.Type,
					"description", variable.Description,
					"writeable", variable.Writeable,
					"maximum_length", variable.MaximumLength,
					"original_type", variable.OriginalType,
				)
				path := strings.Split(variable.Name, ".")
				if path[0] == "device" {
					device[path[1]] = fmt.Sprintf("%v", variable.Value)
				}

				/* Done special processing - now get as general as possible and gather all requested or number-like metrics */
				if len(c.opts.Variables) == 0 || sliceContains(c.opts.Variables, variable.Name) {
					level.Debug(c.logger).Log("msg", "Export the variable? true")
					value := float64(0)

					/* Deal with ups.status specially because it is a collection of 'flags' */
					if variable.Name == "ups.status" {
						setStatuses := make(map[string]bool)
						varDesc := prometheus.NewDesc(prometheus.BuildFQName(c.opts.Namespace, "", strings.Replace(variable.Name, ".", "_", -1)),
							fmt.Sprintf("%s (%s)", variable.Description, variable.Name),
							[]string{"flag"}, nil,
						)

						for _, statusFlag := range strings.Split(variable.Value.(string), " ") {
							setStatuses[statusFlag] = true
							ch <- prometheus.MustNewConstMetric(varDesc, prometheus.GaugeValue, float64(1), statusFlag)
						}

						/* If the user specifies the statues that must always be set, handle that here */
						if len(c.opts.Statuses) > 0 {
							for _, status := range c.opts.Statuses {
								/* This status flag was set because we saw it in the output... skip it */
								if _, ok := setStatuses[status]; ok {
									continue
								}
								ch <- prometheus.MustNewConstMetric(varDesc, prometheus.GaugeValue, float64(0), status)
							}
						}
						continue
					}

					/* All numbers are coaxed to native types by the library, so at this point we know
					   we cannot set this value because a string will never be a float-like number */
					if strings.ToLower(variable.Type) == "string" {
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
					case string:
						/* Nothing we can do here. Bug in nut client library
						   listing UNKNOWN or NUMBER instead of STRING? */
						continue
					default:
						level.Warn(c.logger).Log("Unknonw variable type from nut client library", "name", variable.Name, "type", fmt.Sprintf("%T", v), "claimed_type", variable.Type, "value", v)
						continue
					}

					name := strings.Replace(variable.Name, ".", "_", -1)
					name = strings.Replace(name, "-", "_", -1)

					varDesc := prometheus.NewDesc(prometheus.BuildFQName(c.opts.Namespace, "", name),
						fmt.Sprintf("%s (%s)", variable.Description, variable.Name),
						nil, nil,
					)

					ch <- prometheus.MustNewConstMetric(varDesc, prometheus.GaugeValue, value)
				} else {
					level.Debug(c.logger).Log("msg", "Export the variable? false", "count", len(c.opts.Variables), "variables", c.opts.Variables)
				}
			}

			deviceValues := []string{}
			for _, label := range deviceLabels {
				deviceValues = append(deviceValues, device[label])
			}
			ch <- prometheus.MustNewConstMetric(c.deviceDesc, prometheus.GaugeValue, float64(1), deviceValues...)
		}
		client.Disconnect()
	}
}

func (c *NutCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.deviceDesc
}

func sliceContains(c []string, value string) bool {
	for _, sliceValue := range c {
		if sliceValue == value {
			return true
		}
	}
	return false
}
