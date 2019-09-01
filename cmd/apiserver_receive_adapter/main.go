/*
Copyright 2019 The Knative Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"

	// Uncomment the following line to load the gcp plugin
	// (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"knative.dev/eventing/pkg/adapter/apiserver"
	"knative.dev/eventing/pkg/kncloudevents"
	"knative.dev/eventing/pkg/tracing"
	"knative.dev/eventing/pkg/utils"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/signals"
)

const (
	component = "apiserversource"
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
)

type envConfig struct {
	Namespace         string   `envconfig:"SYSTEM_NAMESPACE" default:"default"`
	Mode              string   `envconfig:"MODE"`
	SinkURI           string   `split_words:"true" required:"true"`
	ApiVersion        []string `split_words:"true" required:"true"`
	Kind              []string `required:"true"`
	Controller        []bool   `required:"true"`
	ApiServerImporter string   `envconfig:"APISERVERIMPORTER" required:"true"`
	// MetricsConfigBase64 is a base64 encoded json string of
	// metrics.ExporterOptions. This is used to configure the metrics exporter
	// options, the config is stored in a config map inside the controllers
	// namespace and copied here.
	MetricsConfigBase64 string `envconfig:"K_METRICS_CONFIG" required:"true"`

	// LoggingConfigBase64 is a base64 encoded json string of logging.Config.
	// This is used to configure the logging config, the config is stored in
	// a config map inside the controllers namespace and copied here.
	LoggingConfigBase64 string `envconfig:"K_LOGGING_CONFIG" required:"true"`
}

// TODO: the controller should take the list of GVR

func main() {
	flag.Parse()

	var env envConfig
	err := envconfig.Process("", &env)
	if err != nil {
		panic(fmt.Sprintf("Error processing env var: %s", err))
	}

	// Convert base64 encoded json logging.Config to logging.Config.
	loggingConfig, err := utils.Base64ToLoggingConfig(
		env.LoggingConfigBase64)
	if err != nil {
		fmt.Printf("[ERROR] filed to process logging config: %s", err.Error())
		// Use default logging config.
		if loggingConfig, err = logging.NewConfigFromMap(map[string]string{}); err != nil {
			// If this fails, there is no recovering.
			panic(err)
		}
	}
	logger, _ := logging.NewLoggerFromConfig(loggingConfig, component)
	defer flush(logger)

	// Convert base64 encoded json metrics.ExporterOptions to
	// metrics.ExporterOptions.
	metricsConfig, err := utils.Base64ToMetricsOptions(
		env.MetricsConfigBase64)
	if err != nil {
		logger.Errorf("failed to process metrics options: %s", err.Error())
	}

	if err := metrics.UpdateExporter(*metricsConfig, logger); err != nil {
		logger.Fatalf("Failed to create the metrics exporter: %v", err)
	}

	reporter, err := apiserver.NewStatsReporter()
	if err != nil {
		logger.Fatalw("Error building statsreporter", zap.Error(err))
	}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		logger.Fatalw("Error building kubeconfig", zap.Error(err))
	}

	logger.Info("Starting the controller")
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		logger.Fatalw("Error building dynamic client", zap.Error(err))
	}

	if err = tracing.SetupStaticPublishing(logger, "apiserversource",
		tracing.OnePercentSampling); err != nil {
		// If tracing doesn't work, we will log an error, but allow the importer
		// to continue to start.
		logger.Error("Error setting up trace publishing", zap.Error(err))
	}

	eventsClient, err := kncloudevents.NewDefaultClient(env.SinkURI)
	if err != nil {
		logger.Fatalw("Error building cloud event client", zap.Error(err))
	}

	gvrcs := []apiserver.GVRC(nil)

	for i, apiVersion := range env.ApiVersion {
		kind := env.Kind[i]
		controlled := env.Controller[i]

		gv, err := schema.ParseGroupVersion(apiVersion)
		if err != nil {
			logger.Fatalw("Error parsing APIVersion", zap.Error(err))
		}
		// TODO: pass down the resource and the kind so we do not have to guess.
		gvr, _ := meta.UnsafeGuessKindToResource(schema.GroupVersionKind{
			Kind:    kind,
			Group:   gv.Group,
			Version: gv.Version})
		gvrcs = append(gvrcs, apiserver.GVRC{
			GVR:        gvr,
			Controller: controlled,
		})
	}

	opt := apiserver.Options{
		Namespace: env.Namespace,
		Mode:      env.Mode,
		GVRCs:     gvrcs,
	}

	a := apiserver.NewAdaptor(cfg.Host, client, eventsClient, logger, opt,
		reporter, env.ApiServerImporter)
	logger.Info("starting kubernetes api adapter.", zap.Any("adapter", env))
	if err := a.Start(stopCh); err != nil {
		logger.Warn("start returned an error,", zap.Error(err))
	}
}

func flush(logger *zap.SugaredLogger) {
	_ = logger.Sync()
	metrics.FlushExporter()
}
