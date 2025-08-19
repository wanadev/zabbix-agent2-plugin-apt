package plugin

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/go-co-op/gocron"
	"golang.zabbix.com/sdk/conf"
	"golang.zabbix.com/sdk/errs"
	"golang.zabbix.com/sdk/metric"
	"golang.zabbix.com/sdk/plugin"
	"golang.zabbix.com/sdk/plugin/container"
)

type MetricKey string

const (
	PluginName = "APT"

	metricPackagesUpgradeSummaryCount = MetricKey("apt.packagesupgradesummary.count")
	metricPackagesUpgradeSummaryDesc  = MetricKey("apt.packagesupgradesummary.description")
	metricPackagesKeptBackDesc        = MetricKey("apt.keptbackupdates.description")
	metricPackagesNewlyInstalledDesc  = MetricKey("apt.newlyinstalled.description")
	metricPackagesReadyToUpgradeDesc  = MetricKey("apt.readytoupgrade.description")
	metricPackagesToRemoveDesc        = MetricKey("apt.toremove.description")
)

type Options struct {
	plugin.SystemOptions `conf:"optional,name=System"`

	Interval int `conf:"optional,range=1:1440,default=1"`
}

type Plugin struct {
	plugin.Base
	updatesCount       int
	updatesSumDesc     string
	keptBackDesc       string
	newlyInstalledDesc string
	readyToUpgradeDesc string
	toRemoveDesc       string
	scheduler          *gocron.Scheduler
	options            Options
}

func (p *Plugin) Export(key string, _ []string, _ plugin.ContextProvider) (result any, err error) {
	switch key {
	case string(metricPackagesUpgradeSummaryCount):
		return p.updatesCount, nil
	case string(metricPackagesUpgradeSummaryDesc):
		return p.updatesSumDesc, nil
	case string(metricPackagesKeptBackDesc):
		return p.keptBackDesc, nil
	case string(metricPackagesNewlyInstalledDesc):
		return p.newlyInstalledDesc, nil
	case string(metricPackagesReadyToUpgradeDesc):
		return p.readyToUpgradeDesc, nil
	case string(metricPackagesToRemoveDesc):
		return p.toRemoveDesc, nil
	default:
		return nil, plugin.UnsupportedMetricError
	}
}

func convertStringToNumber(str string) (int, error) {
	num, err := strconv.Atoi(str)
	if err != nil {
		errs.Wrap(err, "failed to convert string to number")
	}

	return num, err
}

func getMetricsFromOutput(output string) ([]int, error) {
	// summarized output of apt upgrade looks like
	// "0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded."
	// Beware that it can change in the future, if apt output version is configured differently
	// see https://salsa.debian.org/apt-team/apt/-/blob/main/apt-private/private-output.cc?ref_type=heads
	re := regexp.MustCompile(`(\d+) upgraded, (\d+) newly installed, (\d+) to remove and (\d+) not upgraded.`)
	match := re.FindStringSubmatch(output)

	if len(match) != 4 {
		return nil, errors.New("failed to parse upgrade output, wrong format")
	}
	// convert
	var numbers []int
	for _, number := range match {
		integer, err := convertStringToNumber(number)
		if err != nil {
			return nil, errs.Wrap(err, "failed to convert string to number")
		}
		numbers = append(numbers, integer)
	}

	return numbers, nil
}

func getDescFromOutput(output string) []string {
	var results []string
	// If numbers are different than 0, there can be other paragraphs listing packages
	// get the list of packages

	upgradedRe := regexp.MustCompile(`will be upgraded:\n((  [0-9a-zA-Z-_. ]+\n)+)`)
	upgradedMatch := upgradedRe.FindStringSubmatch(output)
	if len(upgradedMatch) != 0 {
		results = append(results, upgradedMatch[1])
	} else {
		results = append(results, "No packages to upgrade")
	}

	newlyInstalledRe := regexp.MustCompile(`NEW packages will be installed:\n((  [0-9a-zA-Z-_. ]+\n)+)`)
	newlyInstalledMatch := newlyInstalledRe.FindStringSubmatch(output)
	if len(newlyInstalledMatch) != 0 {
		results = append(results, newlyInstalledMatch[1])
	} else {
		results = append(results, "No new packages to install")
	}

	toRemoveRe := regexp.MustCompile(`will be REMOVED:\n((  [0-9a-zA-Z-_. ]+\n)+)`)
	toRemoveMatch := toRemoveRe.FindStringSubmatch(output)
	if len(toRemoveMatch) != 0 {
		results = append(results, toRemoveMatch[1])
	} else {
		results = append(results, "No packages to remove")
	}

	keptBackRe := regexp.MustCompile(`have been kept back:\n((  [0-9a-zA-Z-_. ]+\n)+)`)
	keptBackMatch := keptBackRe.FindStringSubmatch(output)
	if len(keptBackMatch) != 0 {
		results = append(results, keptBackMatch[1])
	} else {
		results = append(results, "No packages were kept back")
	}

	return results
}

var updateMetrics = func(p *Plugin) {
	fmt.Println("updateMetrics")
	p.Debugf("updateMetrics")

	upgradeCommand := "apt-get -s upgrade"

	out, err := exec.Command("bash", "-c", upgradeCommand).Output()
	if err != nil {
		p.Errf("cannot execute .: %s", err)
		return
	}

	numbers, err := getMetricsFromOutput(string(out))
	if err != nil {
		p.Errf("cannot parse: %s", err)
		return
	}
	totalPackageCount := numbers[0] + numbers[1]

	descriptions := getDescFromOutput(string(out))

	// save the results
	p.updatesCount = totalPackageCount
	p.updatesSumDesc = fmt.Sprintf("%d upgraded", totalPackageCount)

	p.keptBackDesc = descriptions[3]
	p.newlyInstalledDesc = descriptions[1]
	p.readyToUpgradeDesc = descriptions[0]
	p.toRemoveDesc = descriptions[2]

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
	fmt.Println("Configure")
	if err := conf.Unmarshal(options, &p.options); err != nil {
		p.Errf("cannot unmarshal configuration options: %s", err)
	}
}

func (p *Plugin) Validate(options any) error {
	fmt.Println("Validate")
	var opts Options

	err := conf.Unmarshal(options, &opts)
	if err != nil {
		return errs.Wrap(err, "failed to unmarshal configuration options")
	}

	return nil
}

var metrics = metric.MetricSet{
	string(metricPackagesUpgradeSummaryCount): metric.New("Available updates summary count", []*metric.Param{}, false),
	string(metricPackagesUpgradeSummaryDesc):  metric.New("Available updates summary description", []*metric.Param{}, false),
	string(metricPackagesKeptBackDesc):        metric.New("Kept back packages description", []*metric.Param{}, false),
	string(metricPackagesNewlyInstalledDesc):  metric.New("Newly installed packages description", []*metric.Param{}, false),
	string(metricPackagesReadyToUpgradeDesc):  metric.New("Available updates description", []*metric.Param{}, false),
	string(metricPackagesToRemoveDesc):        metric.New("Packages to be removed description", []*metric.Param{}, false),
}

func Launch() error {
	p := &Plugin{
		updatesCount:       0,
		updatesSumDesc:     "",
		keptBackDesc:       "",
		newlyInstalledDesc: "",
		readyToUpgradeDesc: "",
		toRemoveDesc:       "",
		scheduler:          gocron.NewScheduler(time.UTC),
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
