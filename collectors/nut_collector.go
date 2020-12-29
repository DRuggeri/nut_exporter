package collectors

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/robbiet480/go.nut"
	"strings"
)

var deviceLabels = []string{"model", "mfr", "serial", "type", "description", "contact", "location", "part", "macaddr"}

type NutCollector struct {
	deviceDesc *prometheus.Desc
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

func NewNutCollector(opts NutCollectorOpts) (*NutCollector, error) {
	deviceDesc := prometheus.NewDesc(prometheus.BuildFQName(opts.Namespace, "", "device_info"),
		"UPS Device information",
		deviceLabels, nil,
	)

	return &NutCollector{
		deviceDesc: deviceDesc,
		opts:       opts,
	}, nil
}

func (c *NutCollector) Collect(ch chan<- prometheus.Metric) {
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
	} else {
		upsList := []nut.UPS{}
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

		if err != nil {
			ch <- prometheus.NewInvalidMetric(
				prometheus.NewDesc(prometheus.BuildFQName(c.opts.Namespace, "", "error"),
					"Failure gathering UPS variables", nil, nil),
				err)
		}

		if len(upsList) > 1 {
			log.Errorf("Multiple UPS devices were found by NUT for this scrap. For this configuration, you MUST scrape this exporter with a query string parameter indicating which UPS to scrape. Valid values of ups are:")
			for _, ups := range upsList {
				log.Errorf("  %s", ups.Name)
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

			log.Debugf("UPS info:")
			log.Debugf("  Name: %v", ups.Name)
			log.Debugf("  Description: %v", ups.Description)
			log.Debugf("  Master: %v", ups.Master)
			log.Debugf("  NumberOfLogins: %v", ups.NumberOfLogins)
			log.Debug("  Clients:")
			for i, clientName := range ups.Clients {
				log.Debugf("    %v: %v", i, clientName)
			}
			log.Debug("  Commands:")
			for _, command := range ups.Commands {
				log.Debugf("    %v: %v", command.Name, command.Description)
			}
			log.Debug("  Variables:")
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

					/* Deal with ups.status specially because it is a collection of 'flags' */
					if variable.Name == "ups.status" {
						setStatuses := make(map[string]bool)
						varDesc := prometheus.NewDesc(prometheus.BuildFQName(c.opts.Namespace, "", strings.Replace(variable.Name, ".", "_", -1)),
							fmt.Sprintf("Value of the %s variable from Network UPS Tools", variable.Name),
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
						log.Warnf("Variable from nut client library `%s` is of unknown type `%T` (claimed=`%v` value=`%v`)", variable.Name, v, variable.Type, v)
						continue
					}

					varDesc := prometheus.NewDesc(prometheus.BuildFQName(c.opts.Namespace, "", strings.Replace(variable.Name, ".", "_", -1)),
						fmt.Sprintf("Value of the %s variable from Network UPS Tools", variable.Name),
						nil, nil,
					)

					ch <- prometheus.MustNewConstMetric(varDesc, prometheus.GaugeValue, value)
				} else {
					log.Debugf("      Export the variable? false (%v) (%v)", len(c.opts.Variables), c.opts.Variables)
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
