# Network UPS Tools (NUT) Prometheus Exporter

A [Prometheus](https://prometheus.io) exporter for the Network UPS Tools server. This exporter utilizes the [go.nut](https://github.com/robbiet480/go.nut) project as a network client of the NUT platform. The exporter is written in a way to permit an administrator to scrape one or many UPS devices visible to a NUT client as well as one or all NUT variables. A single instance of this exporter can scrape one or many NUT servers as well.

## Variables and information
The variables exposed to a NUT client by the NUT system are the lifeblood of a deployment. These variables are consumed by this exporter and coaxed to Prometheus types.

 * See the [NUT documentation](https://networkupstools.org/docs/user-manual.chunked/apcs01.html) for a list of all possible variables
 * Variables are set as prometheus metrics with the `ups` name added as a lable. Example: `ups.load` is set as `network_ups_tools_ups_load 100`
 * The exporter SHOULD be called with the ups to scrape set in the query string. Example: `https://127.0.0.1:9199/ups_metrics?ups=foo`
 * If the exporter scrapes NUT and detects more than one UPS, it is an error condition that will fail the scrape. In this case, use a variant of the scrape config example below for your environment
 * Default configs usually permit reading variables without authentication. If you have disabled this, see the Usage below to set credentials
 * This exporter will always export the device.* metrics as labels, except for uptime, with a constant value of 1
 * Setting the `nut.vars_enable` parameter to an empty string will cause all numeric variables to be exported
 * Not all driver and UPS implementations provide all variables. Run this exporter with log.level at debug or use the `LIST VAR` upsc command to see available variables for your UPS
 * All number-like values are coaxed to the appropriate go type by the library and are set as the value of the exported metric
 * Boolean values are coaxed to 0 (false) or 1 (true)
 * The special `ups.status` variable is returned by NUT as a string. It is coaxed to an integer by this exporter to enable use of alerting on status changes.
   * Values are gleaned from the [NUT driver documentation](https://github.com/networkupstools/nut/blob/master/docs/new-drivers.txt)
   * NOTE: Not all UPSs utilize all values! It depends greatly on the driver and the intelligence of the UPS. A general failsafe is to alert when the value is greater than either `0` or `1`
   * The values are:
     * OL - `0` - On line (mains is present)
     * OB - `1` - On battery (mains is not present)
     * LB - `2` - Low battery
     * HB - `3` - High battery
     * RB - `4` - The battery needs to be replaced
     * CHRG - `5` - The battery is charging
     * DISCHRG - `6` - The battery is discharging (inverter is providing load power)
     * BYPASS - `7` -  UPS bypass circuit is active -- no battery protection is available
     * CAL - `8` - UPS is currently performing runtime calibration (on battery)
     * OFF - `9` - UPS is offline and is not supplying power to the load
     * OVER - `10` - UPS is overloaded
     * TRIM - `11` - UPS is trimming incoming voltage (called "buck" in some hardware)
     * BOOST - `12` - UPS is boosting incoming voltage
     * FSD and SD - `13` - Forced Shutdown
     * Any other value - 100 - Unknown

### Query String Parameters
The exporter allows for per-scrape overrides of command line parameters by passing query string parameters. This enables a single nut_exporter to scrape multiple NUT servers

The following query string parameters can be passed to the `/ups_metrics` path:
  * `ups` - Required if more than one UPS is present in NUT)
  * `server` - Overrides the command line parameter `--nut.server`
  * `username` - Overrides the command line parameter `--nut.username`
  * `password` - Overrides the environment variable NUT_EXPORTER_PASSWORD. It is **strongly** recommended to avoid passing credentials over http unless the exporter is configured with TLS
  * `variables` - Overrides the command line parameter `--nut.vars_enable`
See the example scrape configurations below for how to utilize this capability

### Example Scrape Configurations
Note that this exporter will scrape only one UPS per scrape invocation. If there are multiple UPS devices visible to NUT, you MUST ensure that you set up different scrape configs for each UPS device. Here is an example configuration for such a use case:

```
  - job_name: nut-primary
    metrics_path: /ups_metrics
    static_configs:
      - targets: ['myserver:9199']
        labels:
          ups:  "primary"
    params:
      ups: [ "primary" ]
  - job_name: nut-secondary
    metrics_path: /ups_metrics
    static_configs:
      - targets: ['myserver:9199']
        labels:
          ups:  "secondary"
    params:
      ups: [ "secondary" ]
```

You can also configure a single exporter to scrape several NUT servers like so:
```
  - job_name: nut-primary
    metrics_path: /ups_metrics
    static_configs:
      - targets: ['exporterserver:9199']
        labels:
          ups:  "primary"
    params:
      ups: [ "primary" ]
      server: [ "nutserver1" ]
  - job_name: nut-secondary
    metrics_path: /ups_metrics
    static_configs:
      - targets: ['exporterserver:9199']
        labels:
          ups:  "secondary"
    params:
      ups: [ "secondary" ]
      server: [ "nutserver2" ]
```


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
      --nut.username=NUT.USERNAME
                                If set, will authenticate with this username to the server. Password must be set in NUT_EXPORTER_PASSWORD environment variable.' ($NUT_EXPORTER_USERNAME)
      --nut.vars_enable="battery.charge,battery.voltage,battery.voltage.nominal,input.voltage,input.voltage.nominal,ups.load"
                                A comma-separated list of variable names to monitor. See the variable notes in README.' ($NUT_EXPORTER_VARIABLES)
      --metrics.namespace="network_ups_tools"
                                Metrics Namespace ($NUT_EXPORTER_METRICS_NAMESPACE)
      --web.listen-address=":9199"
                                Address to listen on for web interface and telemetry ($NUT_EXPORTER_WEB_LISTEN_ADDRESS)
      --web.telemetry-path="/ups_metrics"
                                Path under which to expose the UPS Prometheus metrics ($NUT_EXPORTER_WEB_TELEMETRY_PATH)
      --web.exporter-telemetry-path="/metrics"
                                Path under which to expose process metrics about this exporter ($NUT_EXPORTER_WEB_EXPORTER_TELEMETRY_PATH)
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
  network_ups_tools_device_info - UPS device information
  network_ups_tools_VARIABLE_NAME - Variable from Network UPS Tools as noted in the variable notes above
```
