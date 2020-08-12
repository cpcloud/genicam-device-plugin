package device

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/device"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
	"github.com/kr/pretty"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// pluginName is the deviceName of the plugin
	// this is used for logging and (along with the version) for uniquely identifying
	// plugin binaries fingerprinted by the client
	pluginName = "genicam-device"

	// plugin version allows the client to identify and use newer versions of
	// an installed plugin
	pluginVersion = "v0.0.1"

	// vendor is the label for the vendor providing the devices.
	// along with "type" and "model", this can be used when requesting devices:
	//   https://www.nomadproject.io/docs/job-specification/device.html#name
	vendor = "tis"

	// deviceType is the "type" of device being returned
	deviceType = "genicam"
)

var (
	// pluginInfo provides information used by Nomad to identify the plugin
	pluginInfo = &base.PluginInfoResponse{
		Type:              base.PluginTypeDevice,
		PluginApiVersions: []string{device.ApiVersion010},
		PluginVersion:     pluginVersion,
		Name:              pluginName,
	}

	// configSpec is the specification of the schema for this plugin's config.
	// this is used to validate the HCL for the plugin provided
	// as part of the client config:
	//   https://www.nomadproject.io/docs/configuration/plugin.html
	// options are here:
	//   https://github.com/hashicorp/nomad/blob/v0.10.0/plugins/shared/hclspec/hcl_spec.proto
	configSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"fingerprint_period": hclspec.NewDefault(
			hclspec.NewAttr("fingerprint_period", "string", false),
			hclspec.NewLiteral("\"1m\""),
		),
	})
)

// Config contains configuration information for the plugin.
type Config struct {
	FingerprintPeriod string `codec:"fingerprint_period"`
}

// GenicamDevicePlugin contains a skeleton for most of the implementation of a
// device plugin.
type GenicamDevicePlugin struct {
	logger log.Logger

	// fingerprintPeriod the period for the fingerprinting loop
	// most plugins that fingerprint in a polling loop will have this
	fingerprintPeriod time.Duration

	// devices is a list of fingerprinted devices
	// most plugins will maintain, at least, a list of the devices that were
	// discovered during fingerprinting.
	// we'll save the "device serial"/"ip address"
	devices    map[string]string
	deviceLock sync.RWMutex
}

// NewPlugin returns a device plugin, used primarily by the main wrapper
//
// Plugin configuration isn't available yet, so there will typically be
// a limit to the initialization that can be performed at this point.
func NewPlugin(log log.Logger) *GenicamDevicePlugin {
	return &GenicamDevicePlugin{
		logger:  log.Named(pluginName),
		devices: make(map[string]string),
	}
}

// PluginInfo returns information describing the plugin.
//
// This is called during Nomad client startup, while discovering and loading
// plugins.
func (d *GenicamDevicePlugin) PluginInfo() (*base.PluginInfoResponse, error) {
	return pluginInfo, nil
}

// ConfigSchema returns the configuration schema for the plugin.
//
// This is called during Nomad client startup, immediately before parsing
// plugin config and calling SetConfig
func (d *GenicamDevicePlugin) ConfigSchema() (*hclspec.Spec, error) {
	return configSpec, nil
}

// SetConfig is called by the client to pass the configuration for the plugin.
func (d *GenicamDevicePlugin) SetConfig(c *base.Config) error {

	// decode the plugin config
	var config Config
	if err := base.MsgPackDecode(c.PluginConfig, &config); err != nil {
		return err
	}

	// save the configuration to the plugin
	// typically, we'll perform any additional validation or conversion
	// from MsgPack base types
	if config.SomeString == "" {
		return fmt.Errorf("some_optional_string_with_default was not acceptible, cannot be empty",
			"value", config.SomeString)
	}
	d.someString = config.SomeString
	d.someBool = config.SomeBool
	d.someIntArray = config.SomeIntArray

	// for example, convert the poll period from an HCL string into a time.Duration
	period, err := time.ParseDuration(config.FingerprintPeriod)
	if err != nil {
		return fmt.Errorf("failed to parse doFingerprint period %q: %v", config.FingerprintPeriod, err)
	}
	d.fingerprintPeriod = period

	d.logger.Debug("test debug")
	d.logger.Info("config set", "config", log.Fmt("% #v", pretty.Formatter(config)))
	return nil
}

// Fingerprint streams detected devices.
// Messages should be emitted to the returned channel when there are changes
// to the devices or their health.
func (d *GenicamDevicePlugin) Fingerprint(ctx context.Context) (<-chan *device.FingerprintResponse, error) {
	// Fingerprint returns a channel. The recommended way of organizing a plugin
	// is to pass that into a long-running goroutine and return the channel immediately.
	outCh := make(chan *device.FingerprintResponse)
	go d.doFingerprint(ctx, outCh)
	return outCh, nil
}

// Stats streams statistics for the detected devices.
// Messages should be emitted to the returned channel on the specified interval.
func (d *GenicamDevicePlugin) Stats(ctx context.Context, interval time.Duration) (<-chan *device.StatsResponse, error) {
	// Similar to Fingerprint, Stats returns a channel. The recommended way of
	// organizing a plugin is to pass that into a long-running goroutine and
	// return the channel immediately.
	outCh := make(chan *device.StatsResponse)
	go d.doStats(ctx, outCh, interval)
	return outCh, nil
}

type reservationError struct {
	notExistingIDs []string
}

func (e *reservationError) Error() string {
	return fmt.Sprintf("unknown device IDs: %s", strings.Join(e.notExistingIDs, ","))
}

// Reserve returns information to the task driver on on how to mount the given devices.
// It may also perform any device-specific orchestration necessary to prepare the device
// for use. This is called in a pre-start hook on the client, before starting the workload.
func (d *GenicamDevicePlugin) Reserve(deviceIDs []string) (*device.ContainerReservation, error) {
	if len(deviceIDs) == 0 {
		return &device.ContainerReservation{}, nil
	}

	// This pattern can be useful for some drivers to avoid a race condition where a device disappears
	// after being scheduled by the server but before the server gets an update on the fingerprint
	// channel that the device is no longer available.
	d.deviceLock.RLock()
	var notExistingIDs []string
	for _, id := range deviceIDs {
		if _, deviceIDExists := d.devices[id]; !deviceIDExists {
			notExistingIDs = append(notExistingIDs, id)
		}
	}
	d.deviceLock.RUnlock()
	if len(notExistingIDs) != 0 {
		return nil, &reservationError{notExistingIDs}
	}

	// initialize the response
	resp := &device.ContainerReservation{
		Envs:    map[string]string{},
		Mounts:  []*device.Mount{},
		Devices: []*device.DeviceSpec{},
	}

	for i, serial_nbr := range deviceIDs {
		// Check if the device is known
		if _, address := d.devices[serial_nbr]; !address {
			return nil, status.Newf(codes.InvalidArgument, "unknown device %q", serial_nbr).Err()
		}

		// Envs are a set of environment variables to set for the task.
		resp.Envs[fmt.Sprintf("DEVICE_%d_SERIAL_NBR", i)] = id
		resp.Envs[fmt.Sprintf("DEVICE_%d_ADDRESS", i)] = address
	}

	return resp, nil
}
