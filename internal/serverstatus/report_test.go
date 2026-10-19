package serverstatus

import (
	"strings"
	"testing"
)

const validReport = `{"home":"/home/me/.kit","pid":42,"url":"http://127.0.0.1:61342","kitVersion":"0.48.0","protocolVersion":47,"instanceId":"instance_1","databaseReady":true,"token":"secret","future":1}`

func TestDecodeReadsAReportAndIgnoresNewFields(t *testing.T) {
	report, err := Decode([]byte(validReport))
	if err != nil {
		t.Fatal(err)
	}
	want := Report{Home: "/home/me/.kit", PID: 42, URL: "http://127.0.0.1:61342", KitVersion: "0.48.0", ProtocolVersion: 47, InstanceID: "instance_1", DatabaseReady: true, Token: "secret"}
	if report != want {
		t.Fatalf("report = %#v", report)
	}
	host, port, err := report.Address()
	if err != nil || host != "127.0.0.1" || port != 61342 {
		t.Fatalf("address = %q %d %v", host, port, err)
	}
}

func TestDecodeRejectsInvalidReports(t *testing.T) {
	for name, change := range map[string][2]string{
		"https url":    {`"http://127.0.0.1:61342"`, `"https://127.0.0.1:61342"`},
		"no port":      {`"http://127.0.0.1:61342"`, `"http://127.0.0.1"`},
		"path":         {`"http://127.0.0.1:61342"`, `"http://127.0.0.1:61342/x"`},
		"option host":  {`"http://127.0.0.1:61342"`, `"http://-oProxy:1"`},
		"elsewhere":    {`"http://127.0.0.1:61342"`, `"http://10.0.0.5:61342"`},
		"c1 control":   {`"kitVersion":"0.48.0"`, `"kitVersion":"0.48\u0085"`},
		"no token":     {`"token":"secret"`, `"token":""`},
		"control text": {`"kitVersion":"0.48.0"`, `"kitVersion":"0.48\u001b"`},
		"no pid":       {`"pid":42`, `"pid":0`},
	} {
		if _, err := Decode([]byte(strings.Replace(validReport, change[0], change[1], 1))); err == nil {
			t.Fatalf("%s: Decode accepted the report", name)
		}
	}
}
