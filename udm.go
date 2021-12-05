package datadogunifi

import (
	"strconv"
	"strings"

	"github.com/unpoller/unifi"
)

// udmT is used as a name for printed/logged counters.
const udmT = item("UDM")

// Combine concatenates N maps. This will delete things if not used with caution.
func Combine(in ...map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})

	for i := range in {
		for k := range in[i] {
			out[k] = in[i][k]
		}
	}

	return out
}

// CombineFloat64 concatenates N maps. This will delete things if not used with caution.
func CombineFloat64(in ...map[string]float64) map[string]float64 {
	out := make(map[string]float64)

	for i := range in {
		for k := range in[i] {
			out[k] = in[i][k]
		}
	}

	return out
}

// batchSysStats is used by all device types.
func (u *DatadogUnifi) batchSysStats(s unifi.SysStats, ss unifi.SystemStats) map[string]float64 {
	m := map[string]float64{
		"loadavg_1":     s.Loadavg1.Val,
		"loadavg_5":     s.Loadavg5.Val,
		"loadavg_15":    s.Loadavg15.Val,
		"mem_used":      s.MemUsed.Val,
		"mem_buffer":    s.MemBuffer.Val,
		"mem_total":     s.MemTotal.Val,
		"cpu":           ss.CPU.Val,
		"mem":           ss.Mem.Val,
		"system_uptime": ss.Uptime.Val,
	}

	for k, v := range ss.Temps {
		temp, _ := strconv.Atoi(strings.Split(v, " ")[0])
		k = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(k, " ", "_"), ")", ""), "(", "")

		if temp != 0 && k != "" {
			m["temp_"+strings.ToLower(k)] = float64(temp)
		}
	}

	return m
}

func (u *DatadogUnifi) batchUDMtemps(temps []unifi.Temperature) map[string]float64 {
	output := make(map[string]float64)

	for _, t := range temps {
		output["temp_"+t.Name] = t.Value
	}

	return output
}

func (u *DatadogUnifi) batchUDMstorage(storage []*unifi.Storage) map[string]float64 {
	output := make(map[string]float64)

	for _, t := range storage {
		output["storage_"+t.Name+"_size"] = t.Size.Val
		output["storage_"+t.Name+"_used"] = t.Used.Val

		if t.Size.Val != 0 && t.Used.Val != 0 && t.Used.Val < t.Size.Val {
			output["storage_"+t.Name+"_pct"] = t.Used.Val / t.Size.Val * 100 //nolint:gomnd
		} else {
			output["storage_"+t.Name+"_pct"] = 0
		}
	}

	return output
}

// batchUDM generates Unifi Gateway datapoints for Datadog.
// These points can be passed directly to datadog.
func (u *DatadogUnifi) batchUDM(r report, s *unifi.UDM) { // nolint: funlen
	if !s.Adopted.Val || s.Locating.Val {
		return
	}

	tags := cleanTags(map[string]string{
		"source":        s.SourceName,
		"mac":           s.Mac,
		"site_name":     s.SiteName,
		"name":          s.Name,
		"version":       s.Version,
		"model":         s.Model,
		"serial":        s.Serial,
		"type":          s.Type,
		"ip":            s.IP,
		"license_state": s.LicenseState,
	})
	data := CombineFloat64(
		u.batchUDMstorage(s.Storage),
		u.batchUDMtemps(s.Temperatures),
		u.batchUSGstats(s.SpeedtestStatus, s.Stat.Gw, s.Uplink),
		u.batchSysStats(s.SysStats, s.SystemStats),
		map[string]float64{
			"bytes":         s.Bytes.Val,
			"last_seen":     s.LastSeen.Val,
			"guest-num_sta": s.GuestNumSta.Val,
			"rx_bytes":      s.RxBytes.Val,
			"tx_bytes":      s.TxBytes.Val,
			"uptime":        s.Uptime.Val,
			"state":         s.State.Val,
			"user-num_sta":  s.UserNumSta.Val,
			"num_desktop":   s.NumDesktop.Val,
			"num_handheld":  s.NumHandheld.Val,
			"num_mobile":    s.NumMobile.Val,
		},
	)

	r.addCount(udmT)
	metricName := metricNamespace("usg")
	reportGaugeForFloat64Map(r, metricName, data, tags)

	u.batchNetTable(r, tags, s.NetworkTable)
	u.batchUSGwans(r, tags, s.Wan1, s.Wan2)

	tags = cleanTags(map[string]string{
		"mac":       s.Mac,
		"site_name": s.SiteName,
		"source":    s.SourceName,
		"name":      s.Name,
		"version":   s.Version,
		"model":     s.Model,
		"serial":    s.Serial,
		"type":      s.Type,
		"ip":        s.IP,
	})
	data = CombineFloat64(
		u.batchUSWstat(s.Stat.Sw),
		map[string]float64{
			"guest-num_sta": s.GuestNumSta.Val,
			"bytes":         s.Bytes.Val,
			"last_seen":     s.LastSeen.Val,
			"rx_bytes":      s.RxBytes.Val,
			"tx_bytes":      s.TxBytes.Val,
			"uptime":        s.Uptime.Val,
		})

	metricName = metricNamespace("usw")
	reportGaugeForFloat64Map(r, metricName, data, tags)

	u.batchPortTable(r, tags, s.PortTable) // udm has a usw in it.

	if s.Stat.Ap == nil {
		return // we're done now. the following code process UDM (non-pro) UAP data.
	}

	tags = cleanTags(map[string]string{
		"mac":       s.Mac,
		"site_name": s.SiteName,
		"source":    s.SourceName,
		"name":      s.Name,
		"version":   s.Version,
		"model":     s.Model,
		"serial":    s.Serial,
		"type":      s.Type,
		"ip":        s.IP,
	})
	data = u.processUAPstats(s.Stat.Ap)
	data["bytes"] = s.Bytes.Val
	data["last_seen"] = s.LastSeen.Val
	data["rx_bytes"] = s.RxBytes.Val
	data["tx_bytes"] = s.TxBytes.Val
	data["uptime"] = s.Uptime.Val
	data["state"] = s.State.Val
	data["user-num_sta"] = s.UserNumSta.Val
	data["guest-num_sta"] = s.GuestNumSta.Val
	data["num_sta"] = s.NumSta.Val

	metricName = metricNamespace("uap")
	reportGaugeForFloat64Map(r, metricName, data, tags)

	u.processRadTable(r, tags, *s.RadioTable, *s.RadioTableStats)
	u.processVAPTable(r, tags, *s.VapTable)
}
