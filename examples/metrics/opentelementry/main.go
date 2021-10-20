package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/sdk/metric/aggregator/histogram"

	export "go.opentelemetry.io/otel/sdk/export/metric"
	controller "go.opentelemetry.io/otel/sdk/metric/controller/basic"
	processor "go.opentelemetry.io/otel/sdk/metric/processor/basic"
	selector "go.opentelemetry.io/otel/sdk/metric/selector/simple"
)

var (
	KeyMethod = attribute.Key("method")
	KeyStatus = attribute.Key("status")
)

type app struct {
	exporter *prometheus.Exporter

	meter metric.Meter

	latencyMsRecorder  metric.Float64ValueRecorder
	lineLengthRecorder metric.Int64ValueRecorder
	lineCounter        metric.Int64Counter
	lastLineLength     metric.Int64UpDownCounter
}

func (a *app) processHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()
	commonLabels := []attribute.KeyValue{KeyMethod.String(r.Method), KeyStatus.String("OK")}

	line := r.URL.Query().Get("line")
	lineLength := int64(len(line))

	defer func() {
		a.meter.RecordBatch(
			ctx,
			commonLabels,
			a.latencyMsRecorder.Measurement(sinceInMilliseconds(startTime)),
			a.lineLengthRecorder.Measurement(lineLength),
			a.lineCounter.Measurement(1),
			a.lastLineLength.Measurement(lineLength),
		)
	}()

	time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond) // имитация работы

	writeResponse(w, http.StatusOK, strings.ToUpper(line))
}

func (a *app) initMeters() (err error) {
	config := prometheus.Config{}

	c := controller.New(
		processor.New(
			selector.NewWithHistogramDistribution(
				histogram.WithExplicitBoundaries(config.DefaultHistogramBoundaries),
			),
			export.CumulativeExportKindSelector(),
			processor.WithMemory(true),
		),
	)

	a.exporter, err = prometheus.New(config, c)
	if err != nil {
		return fmt.Errorf("failed to initialize prometheus exporter %s", err)
	}

	global.SetMeterProvider(a.exporter.MeterProvider())

	return nil
}

func (a *app) Init() error {
	if err := a.initMeters(); err != nil {
		return err
	}

	a.meter = global.Meter("ex.com/basic")

	// prometheus type: histogram
	a.latencyMsRecorder = metric.Must(a.meter).NewFloat64ValueRecorder(
		"repl/latency",
		metric.WithDescription("The distribution of the latencies"))

	// prometheus type: histogram
	a.lineLengthRecorder = metric.Must(a.meter).NewInt64ValueRecorder(
		"repl/line_lengths",
		metric.WithDescription("Groups the lengths of keys in buckets"))

	// prometheus type: counter
	a.lineCounter = metric.Must(a.meter).NewInt64Counter(
		"repl/line_count",
		metric.WithDescription("Count of lines"))

	// prometheus type: gauge
	a.lastLineLength = metric.Must(a.meter).NewInt64UpDownCounter(
		"repl/last_line_length",
		metric.WithDescription("Last line length"))

	return nil
}

func (a *app) Serve() error {
	mux := http.NewServeMux()
	mux.Handle("/process", http.HandlerFunc(a.processHandler)) // /process?line=текст+тут
	mux.Handle("/metrics", a.exporter)

	return http.ListenAndServe("0.0.0.0:9000", mux)
}

func main() {
	a := app{}

	if err := a.Init(); err != nil {
		log.Fatal(err)
	}

	if err := a.Serve(); err != nil {
		log.Fatal(err)
	}
}

func sinceInMilliseconds(startTime time.Time) float64 {
	return float64(time.Since(startTime).Nanoseconds()) / 1e6
}

func writeResponse(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(message))
	_, _ = w.Write([]byte("\n"))
}
