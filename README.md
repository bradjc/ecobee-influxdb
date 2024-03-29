# Ecobee -> InfluxDB 1.x Connector

Pulls historical data from Ecobee and pushes to an Influx 1.x database.

## Changes from ecobee_influx_connector

This is heavily inspired by https://github.com/cdzombak/ecobee_influx_connector.

The major differences are:

- It pushes to InfluxDB 1.x database rather than InfluxDB 2.
- It pulls historical data rather than current (last 5 minute) data.
- It supports multiple thermostats.

## Getting Started

1. Enable the developer dashboard on your Ecobee account at
   https://www.ecobee.com/developers/ by logging in with your normal ecobee
   credentials.
2. Go to https://www.ecobee.com/consumerportal/index.html, navigate to Developer
   in the right-hand menu, and create an App. Call it whatever you want. Use PIN
   authentication.
3. Create a working directory on your machine for this tool to store your ecobee
   credentials in.
3. Create a JSON config file based on the example containing the API key from
   the ecobee app and the path to your working directory.
4. Run `ecobee_influx_connector -list-thermostats` at an interactive terminal;
   it'll provide a PIN.
5. Go to https://www.ecobee.com/consumerportal/index.html, navigate to My Apps
   in the right-hand menu, and click Add Application.
6. Paste the PIN there and authorize the app.
7. Return to the `ecobee_influx_connector` CLI and hit Enter.

You should then be presented with a list of thermostats in your Ecobee account,
along with their IDs.

## Configure

Configuration is specified in a JSON file. Create a file (based on the template
`config.example.json` stored in this repository) and customize it with your
Ecobee API key, thermostat ID, and Influx server. Note, you may use a comma
separated list of thermostats (no spaces).

Use the `write_*` config fields to tell the connector which pieces of equipment
you use.

The `work_dir` is where client credentials and (yet to be implemented)
last-written watermarks are stored.


## Build

```shell
go build -o ./ecobee_influx_connector .
```

To cross-compile for eg. Linux/amd64:

```shell
env GOOS=linux GOARCH=amd64 go build -o ./ecobee_influx_connector .
```

## Install & Run via systemd on Linux

1. Build the `ecobee_influx_connector` binary per the Build instructions above.
2. Copy it to `/usr/local/bin` or your preferred location.
3. Create a work directory for the connector. (I put this at `$HOME/.ecobee_influx_connector`.)
4. Run `chmod 700 $YOUR_NEW_WORK_DIR`. (For my work directory, I ran `chmod 700 $HOME/.ecobee_influx_connector`.)
5. Create a configuration JSON file, per the Configure instructions above. (I put this at `$HOME/.ecobee_influx_connector/config.json`.)
6. Customize [`ecobee-influx-connector.service`](https://raw.githubusercontent.com/cdzombak/ecobee_influx_connector/main/ecobee-influx-connector.service) with your user/group name and the path to your config file.
7. Copy that customized `ecobee-influx-connector.service` to `/etc/systemd/system`.
8. Run `chown root:root /etc/systemd/system/ecobee-influx-connector.service`.
9. Run `systemctl daemon-reload && systemctl enable ecobee-influx-connector.service && systemctl start ecobee-influx-connector.service`.
10. Check the service's status with `systemctl status ecobee-influx-connector.service`.

## License

Apache 2; see `LICENSE` in this repository.

## Author

- Brad Campbell.
- [Chris Dzombak](https://www.dzombak.com) (GitHub [@cdzombak](https://github.com/cdzombak)).
