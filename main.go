package main

import (
	// "context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/avast/retry-go"
	influxclient "github.com/influxdata/influxdb1-client/v2"

	"ecobee_influx_connector/ecobee" // taken from https://github.com/rspier/go-ecobee and lightly customized
)

type Config struct {
	APIKey                    string `json:"api_key"`
	WorkDir                   string `json:"work_dir,omitempty"`
	ThermostatID              string `json:"thermostat_id"`
	InfluxServer              string `json:"influx_server"`
	InfluxUser                string `json:"influx_user,omitempty"`
	InfluxPass                string `json:"influx_password,omitempty"`
	InfluxDatabase            string `json:"influx_database"`
	InfluxHealthCheckDisabled bool   `json:"influx_health_check_disabled"`
	WriteHeatPump1            bool   `json:"write_heat_pump_1"`
	WriteHeatPump2            bool   `json:"write_heat_pump_2"`
	WriteAuxHeat1             bool   `json:"write_aux_heat_1"`
	WriteAuxHeat2             bool   `json:"write_aux_heat_2"`
	WriteCool1                bool   `json:"write_cool_1"`
	WriteCool2                bool   `json:"write_cool_2"`
	WriteHumidifier           bool   `json:"write_humidifier"`
	AlwaysWriteWeather        bool   `json:"always_write_weather_as_current"`
}

const (
	thermostatNameTag = "thermostat_name"
)

// WindChill calculates the wind chill for the given temperature (in Fahrenheit)
// and wind speed (in miles/hour). If wind speed is less than 3 mph, or temperature
// if over 50 degrees, the given temperature is returned - the forumla works
// below 50 degrees and above 3 mph.
func WindChill(tempF, windSpeedMph float64) float64 {
	if tempF > 50.0 || windSpeedMph < 3.0 {
		return tempF
	}
	return 35.74 + (0.6215 * tempF) - (35.75 * math.Pow(windSpeedMph, 0.16)) + (0.4275 * tempF * math.Pow(windSpeedMph, 0.16))
}

// IndoorHumidityRecommendation returns the maximum recommended indoor relative
// humidity percentage for the given outdoor temperature (in degrees F).
func IndoorHumidityRecommendation(outdoorTempF float64) int {
	if outdoorTempF >= 50 {
		return 50
	}
	if outdoorTempF >= 40 {
		return 45
	}
	if outdoorTempF >= 30 {
		return 40
	}
	if outdoorTempF >= 20 {
		return 35
	}
	if outdoorTempF >= 10 {
		return 30
	}
	if outdoorTempF >= 0 {
		return 25
	}
	if outdoorTempF >= -10 {
		return 20
	}
	return 15
}

func main() {
	configFile := flag.String("config", "", "Configuration JSON file.")
	listThermostats := flag.Bool("list-thermostats", false, "List available thermostats, then exit.")
	flag.Parse()

	if *configFile == "" {
		fmt.Println("-config is required.")
		os.Exit(1)
	}

	config := Config{}
	cfgBytes, err := ioutil.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("Unable to read config file '%s': %s", *configFile, err)
	}
	if err = json.Unmarshal(cfgBytes, &config); err != nil {
		log.Fatalf("Unable to parse config file '%s': %s", *configFile, err)
	}
	if config.APIKey == "" {
		log.Fatal("api_key must be set in the config file.")
	}
	if config.WorkDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Unable to get current working directory: %s", err)
		}
		config.WorkDir = wd
	}

	client := ecobee.NewClient(config.APIKey, path.Join(config.WorkDir, "ecobee-cred-cache"))

	if *listThermostats {
		s := ecobee.Selection{
			SelectionType: "registered",
		}
		ts, err := client.GetThermostats(s)
		if err != nil {
			log.Fatal(err)
		}
		for _, t := range ts {
			fmt.Printf("'%s': ID %s\n", t.Name, t.Identifier)
		}
		os.Exit(0)
	}

	if config.ThermostatID == "" {
		log.Fatalf("thermostat_id must be set in the config file.")
	}
	if config.InfluxServer == "" {
		log.Fatalf("influx_server must be set in the config file.")
	}

	// Influx
	const influxTimeout = 3 * time.Second

	influxClient, err := influxclient.NewHTTPClient(influxclient.HTTPConfig{
		Addr:     config.InfluxServer,
		Username: config.InfluxUser,
		Password: config.InfluxPass,
	})

	doUpdate := func(start_str string, end_str string) {
		if err := retry.Do(
			func() error {
				s := ecobee.Selection{
					SelectionType:  "thermostats",
					SelectionMatch: config.ThermostatID,

					IncludeAlerts:          false,
					IncludeEvents:          false,
					IncludeProgram:         false,
					IncludeRuntime:         false,
					IncludeExtendedRuntime: false,
					IncludeSettings:        false,
					IncludeSensors:         false,
					IncludeWeather:         false,
				}
				thermostats, err := client.GetThermostats(s)
				if err != nil {
					return err
				}

				thermostat_metadata := map[string]map[string]string{}
				for _, t := range thermostats {
					meta := map[string]string{
						"thermostat_name":  t.Name,
						"thermostat_model": t.ModelNumber,
						"thermostat_brand": t.Brand,
					}

					thermostat_metadata[t.Identifier] = meta
				}

				report_data, rr_err := client.GetRuntimeReport(config.ThermostatID,
					start_str, end_str,
					config.WriteHumidifier,
					config.WriteAuxHeat1,
					config.WriteAuxHeat2,
					config.WriteHeatPump1,
					config.WriteHeatPump2,
					config.WriteCool1,
					config.WriteCool2)

				_ = rr_err

				// fmt.Printf("\n\n%v\n\n", report_data);

				for thermostat_id, entries := range report_data {

					meta := map[string]string{
						"device_id": fmt.Sprintf("ecobee-%s", thermostat_id),
						"receiver":  "ecobee-influx-connector",
					}

					// Copy in the thermostat data from the getThermostats call.
					for k, v := range thermostat_metadata[thermostat_id] {
						meta[k] = v
					}

					bp, _ := influxclient.NewBatchPoints(influxclient.BatchPointsConfig{Database: config.InfluxDatabase})

					if entries_ok, ok := entries.([]ecobee.RuntimeReportDataEntry); ok {
						for _, entry := range entries_ok {

							fields := map[string]interface{}{}

							for key, val := range entry.DataFields {
								if key == "auxHeat1" {
									fields["aux_heat_1_run_time_s"], _ = strconv.Atoi(val)
								} else if key == "auxHeat2" {
									fields["aux_heat_2_run_time_s"], _ = strconv.Atoi(val)
								} else if key == "compCool1" {
									fields["cool_1_run_time_s"], _ = strconv.Atoi(val)
								} else if key == "compCool2" {
									fields["cool_2_run_time_s"], _ = strconv.Atoi(val)
								} else if key == "compHeat1" {
									fields["heat_pump_1_run_time_s"], _ = strconv.Atoi(val)
								} else if key == "compHeat2" {
									fields["heat_pump_2_run_time_s"], _ = strconv.Atoi(val)
								} else if key == "humidifier" {
									fields["humidifier_run_time_s"], _ = strconv.Atoi(val)
								} else if key == "zoneCoolTemp" {
									fields["setpoint_cool_°F"], _ = strconv.ParseFloat(val, 64)
								} else if key == "zoneHeatTemp" {
									fields["setpoint_heat_°F"], _ = strconv.ParseFloat(val, 64)
								} else if key == "zoneAveTemp" {
									fields["temperature_°F"], _ = strconv.ParseFloat(val, 64)
								} else if key == "zoneHumidity" {
									fields["humidity_%"], _ = strconv.ParseFloat(val, 64)
								} else if key == "outdoorTemp" {
									fields["outdoor_temperature_°F"], _ = strconv.ParseFloat(val, 64)
								} else if key == "outdoorHumidity" {
									fields["outdoor_humidity_%"], _ = strconv.ParseFloat(val, 64)
								} else if key == "hvacMode" {
									fields["HVAC_mode"] = val
								} else if key == "fan" {
									fields["fan_run_time_s"], _ = strconv.Atoi(val)
								}
							}

							pt, _ := influxclient.NewPoint("ecobee_runtime_report", meta, fields, entry.ReportTime)
							bp.AddPoint(pt)
							// fmt.Printf("added point %v\n", entry.ReportTime);

						}
					}

					fmt.Printf("writing\n")

					err := influxClient.Write(bp)
					if err != nil {
						fmt.Printf("ERROR writing\n")
						fmt.Printf("Unexpected error during Write: %v", err)
						return err
					}
					fmt.Printf("runtime write good\n")

				}

				return nil
			},
		); err != nil {
			log.Fatal(err)
		} else {
			// Update collected time.
			_ = ioutil.WriteFile("./last_data.txt", []byte(end_str+"\n"), 0o644)
		}
	}

	for true {
		// Get the date of the last day we have gotten data for.
		lastDataBytes, _ := ioutil.ReadFile("./last_data.txt")
		lastData := strings.TrimSpace(string(lastDataBytes))

		// See if there is a day that is over that we have not gotten data for yet.
		now := time.Now()
		yesterday_time := now.Add(-24 * time.Hour)
		yesterday_string := yesterday_time.Format("2006-01-02")

		left_off, _ := time.Parse("2006-01-02", lastData)
		yesterday, _ := time.Parse("2006-01-02", yesterday_string)

		if !left_off.Before(yesterday) {
			fmt.Printf("Nothing to do!\n")

			// Go ahead and exit now.
			os.Exit(0)
		}

		// There is data we need to collect and push to influx.

		// Start date is the day after the last day, starting at midnight.
		start := left_off.Add(24 * time.Hour)
		// See if we can do up to 2 weeks of data.
		projected_end := start.Add(14 * 24 * time.Hour)
		end := projected_end
		if projected_end.After(yesterday) {
			// Projected end is into the future. So we just go up until yesterday.
			end = yesterday
		}

		start_str := start.Format("2006-01-02")
		end_str := end.Format("2006-01-02")

		fmt.Printf("Start: %s\n", start_str)
		fmt.Printf("End:   %s\n", end_str)

		doUpdate(start_str, end_str)

		// Wait 3 seconds.
		time.Sleep(3 * time.Second)
	}
}
