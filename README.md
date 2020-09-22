# Network UPS Tools (NUT) Prometheus Exporter

A [Prometheus](https://prometheus.io) exporter for the Network UPS Tools server. This exporter utilizes the [go.nut](https://github.com/robbiet480/go.nut) project as a network client of the NUT platform. The exporter is written in a way to permit an administrator to scrape one or all UPS devices visible to a NUT client as well as one or all NUT variables.

## Variables and information
The variables exposed to a NUT client by the NUT system are the lifeblood of a deployment. These variables are consumed by this exporter and coaxed to Prometheus types.

 * See the [NUT documentation](https://networkupstools.org/docs/user-manual.chunked/apcs01.html) for a list of all possible variables
 * Default configs usually permit reading variables without authentication. If you have disabled this, see the Usage below to set credentials
 * This exporter will always export the device.* metrics as labels with a constant value of 1
 * Not all driver and UPS implementations provide all variables. Run this exporter with log.level at debug or use the `LIST VAR` upsc command to see available variables for your UPS
 * All number-like values are coaxed to the appropriate go type by the library and are set as the value of the exported metric
 * Boolean values are coaxed to 0 (false) or 1 (true)

## Installation

### Binaries

Download the already existing [binaries](https://github.com/DRuggeri/nut_exporter/releases) for your platform:

```bash
$ ./nut_exporter <flags>
```

### From source

Using the standard `go install` (you must have [Go](https://golang.org/) already installed in your local machine):

```bash
$ go install github.com/DRuggeri/nut_exporter
$ nut_exporter <flags>
```

### With Docker
```bash
docker build -t nut_exporter .
docker run -d -p 9199:9199 nut_exporter"
```

## Usage

### Flags

```
usage: nut_exporter [<flags>]

Flags:
  -h, --help                    Show context-sensitive help (also try --help-long and --help-man).
      --nut.server="127.0.0.1"  Hostname or IP address of the server to connect to.' ($NUT_EXPORTER_SERVER)
      --nut.ups=NUT.UPS         Optional name of UPS to monitor. If not set, all UPSs found will be monitored on each scrape' ($NUT_EXPORTER_UPS)
      --nut.username=NUT.USERNAME
                                If set, will authenticate with this username to the server. Password must be set in NUT_EXPORTER_PASSWORD environment variable.' ($NUT_EXPORTER_USERNAME)
      --nut.vars_enable="battery.charge,battery.voltage,battery.voltage.nominal,input.voltage,input.voltage.nominal,ups.load"
                                A comma-separated list of variable names to monitor. See the variable notes in README.' ($NUT_EXPORTER_VARIABLES)
      --metrics.namespace="network_ups_tools"
                                Metrics Namespace ($NUT_EXPORTER_METRICS_NAMESPACE)
      --web.listen-address=":9198"
                                Address to listen on for web interface and telemetry ($NUT_EXPORTER_WEB_LISTEN_ADDRESS)
      --web.telemetry-path="/metrics"
                                Path under which to expose Prometheus metrics ($NUT_EXPORTER_WEB_TELEMETRY_PATH)
      --web.auth.username=WEB.AUTH.USERNAME
                                Username for web interface basic auth ($NUT_EXPORTER_WEB_AUTH_USERNAME)
      --web.tls.cert_file=WEB.TLS.CERT_FILE
                                Path to a file that contains the TLS certificate (PEM format). If the certificate is signed by a certificate authority, the file should be the concatenation of the server's certificate, any intermediates,
                                and the CA's certificate ($NUT_EXPORTER_WEB_TLS_CERTFILE)
      --web.tls.key_file=WEB.TLS.KEY_FILE
                                Path to a file that contains the TLS private key (PEM format) ($NUT_EXPORTER_WEB_TLS_KEYFILE)
      --printMetrics            Print the metrics this exporter exposes and exits. Default: false ($NUT_EXPORTER_PRINT_METRICS)
      --log.level="info"        Only log messages with the given severity or above. Valid levels: [debug, info, warn, error, fatal]
      --log.format="logger:stderr"
                                Set the log target and format. Example: "logger:syslog?appname=bob&local=7" or "logger:stdout?json=true"
      --version                 Show application version.
```

## Metrics

### NUT
This collector is the workhorse of the exporter. Default metrics are exported for the device and scrape stats. The `network_ups_tools_ups_variable` metric is exported with labels of `ups` and `variable` with the value set as noted in the README

```
NUT
  network_ups_tools_ups_device - UPS device information
  network_ups_tools_ups_variable - Variable from Network UPS Tools
  network_ups_tools_ups_scrapes_total - Total number of scrapes for network UPS tools variables
  network_ups_tools_ups_scrape_errors_total - Total number of scrapes errors for Network UPS Tools variables
  network_ups_tools_ups_last_scrape_error - Whether the last scrape of Network UPS Tools variables resulted in an error (1 for error, 0 for success)
  network_ups_tools_ups_last_scrape_timestamp - Number of seconds since 1970 since last scrape of Network UPS Tools variables
  network_ups_tools_ups_last_scrape_duration_seconds - Duration of the last scrape of Network UPS Tools variables
```
