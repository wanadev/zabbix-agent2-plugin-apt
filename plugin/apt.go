package plugin

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-co-op/gocron"
	"golang.zabbix.com/sdk/conf"
	"golang.zabbix.com/sdk/errs"
	"golang.zabbix.com/sdk/metric"
	"golang.zabbix.com/sdk/plugin"
	"golang.zabbix.com/sdk/plugin/container"
)

const (
	PluginName  = "APT"
	keyUpdates  = "apt.updates"
	keySecurity = "apt.security"
)

type Options struct {
	plugin.SystemOptions `conf:"optional,name=System"`

	Interval int `conf:"optional,range=1:1440,default=1"`
}

type Plugin struct {
	plugin.Base
	updates   int
	security  int
	scheduler *gocron.Scheduler
	options   Options
}

func (p *Plugin) Export(key string, _ []string, _ plugin.ContextProvider) (result any, err error) {
	switch key {
	case keyUpdates:
		return p.updates, nil
	case keySecurity:
		return p.security, nil
	default:
		return nil, plugin.UnsupportedMetricError
	}
}

var updateMetrics = func(p *Plugin) {
	p.Debugf("updateMetrics")

	commands := map[string]string{
		keyUpdates:  "apt-get -s upgrade | grep 'upgraded,.*newly installed,' | cut -d ' ' -f1",
		keySecurity: "apt-get -s upgrade | grep 'standard security updates' | cut -d ' ' -f1",
	}

	for key, cmd := range commands {
		out, err := exec.Command("bash", "-c", cmd).Output()

		if err != nil {
			p.Errf("cannot execute %s: %s", key, err)
		}

		packages, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 32)

		switch key {
		case keyUpdates:
			p.updates = int(packages)
			break
		case keySecurity:
			p.security = int(packages)
			break
		}
	}
}

func (p *Plugin) Start() {
	fmt.Println("Starting")
	_, _ = p.scheduler.Every(p.options.Interval).Minutes().StartImmediately().Do(updateMetrics, p)
	p.scheduler.StartAsync()
}

func (p *Plugin) Stop() {
	fmt.Println("Stopping")
	p.scheduler.Stop()
}

func (p *Plugin) Configure(_ *plugin.GlobalOptions, options any) {
	if err := conf.Unmarshal(options, &p.options); err != nil {
		p.Errf("cannot unmarshal configuration options: %s", err)
	}
}

func (p *Plugin) Validate(options any) error {
	var opts Options

	return conf.Unmarshal(options, &opts)
}

var metrics = metric.MetricSet{
	keyUpdates:  metric.New("Available Updates", []*metric.Param{}, false),
	keySecurity: metric.New("Security Updates", []*metric.Param{}, false),
}

func Launch() error {
	p := &Plugin{
		updates:   0,
		security:  0,
		scheduler: gocron.NewScheduler(time.UTC),
	}
	p.scheduler.SetMaxConcurrentJobs(1, gocron.RescheduleMode)

	err := plugin.RegisterMetrics(p, PluginName, metrics.List()...)
	if err != nil {
		return errs.Wrap(err, "failed to register metrics")
	}

	fmt.Println("Creating Plugin Handler")
	h, err := container.NewHandler(PluginName)
	if err != nil {
		return errs.Wrap(err, "failed to create plugin handler")
	}
	p.Logger = &h

	fmt.Println("Execute")
	err = h.Execute()
	if err != nil {
		return errs.Wrap(err, "failed to execute plugin handler")
	}

	return nil
}
