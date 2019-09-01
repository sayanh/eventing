/*
 * Copyright 2019 The Knative Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package apiserver

import (
	"context"

	"knative.dev/pkg/metrics/metricskey"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	metricsKeyEventing "knative.dev/eventing/pkg/metrics/metricskey"
	"knative.dev/pkg/metrics"
)

var (
	// eventCountM is a counter which records the number of events sent
	// by an Importer.
	eventCountM = stats.Int64(
		"event_count",
		"Number of events created",
		stats.UnitDimensionless,
	)
	_ StatsReporter = (*reporter)(nil)
)

type ReportArgs struct {
	ns                string
	eventType         string
	eventSource       string
	apiServerImporter string
}

const (
	importerResourceGroupValue = "apiserversources.sources.eventing.knative.dev"
)

// StatsReporter defines the interface for sending filter metrics.
type StatsReporter interface {
	ReportEventCount(args *ReportArgs, err error) error
}

// reporter holds cached metric objects to report filter metrics.
type reporter struct {
	namespaceTagKey             tag.Key
	eventTypeTagKey             tag.Key
	eventSourceTagKey           tag.Key
	importerNameTagKey          tag.Key
	importerResourceGroupTagKey tag.Key
	resultKey                   tag.Key
}

// NewStatsReporter creates a reporter that collects and reports apiserversource
// metrics.
func NewStatsReporter() (StatsReporter, error) {
	var r = &reporter{}

	// Create the tag keys that will be used to add tags to our measurements.
	nsTag, err := tag.NewKey(metricskey.LabelNamespaceName)
	if err != nil {
		return nil, err
	}
	r.namespaceTagKey = nsTag

	eventTypeTag, err := tag.NewKey(metricskey.LabelEventType)
	if err != nil {
		return nil, err
	}
	r.eventTypeTagKey = eventTypeTag

	eventSourceTag, err := tag.NewKey(metricskey.LabelEventSource)
	if err != nil {
		return nil, err
	}
	r.eventSourceTagKey = eventSourceTag

	importerNameTag, err := tag.NewKey(metricskey.LabelImporterName)
	if err != nil {
		return nil, err
	}
	r.importerNameTagKey = importerNameTag

	importerResourceGroupTag, err := tag.NewKey(metricskey.LabelImporterResourceGroup)
	if err != nil {
		return nil, err
	}
	r.importerResourceGroupTagKey = importerResourceGroupTag

	resultTag, err := tag.NewKey(metricsKeyEventing.Result)
	if err != nil {
		return nil, err
	}
	r.resultKey = resultTag

	// Create view to see our measurements.
	err = view.Register(
		&view.View{
			Description: eventCountM.Description(),
			Measure:     eventCountM,
			Aggregation: view.Count(),
			TagKeys: []tag.Key{r.namespaceTagKey, r.eventSourceTagKey,
				r.eventTypeTagKey, r.importerNameTagKey, r.importerResourceGroupTagKey},
		},
	)
	if err != nil {
		return nil, err
	}

	return r, nil
}

// ReportEventCount captures the event count.
func (r *reporter) ReportEventCount(args *ReportArgs, err error) error {
	ctx, err := r.generateTag(args, tag.Insert(r.resultKey, Result(err)))
	if err != nil {
		return err
	}
	ctx, err = r.generateTag(args, tag.Insert(r.importerResourceGroupTagKey,
		importerResourceGroupValue))
	if err != nil {
		return err
	}
	metrics.Record(ctx, eventCountM.M(1))
	return nil
}

func (r *reporter) generateTag(args *ReportArgs, t tag.Mutator) (context.Context, error) {
	return tag.New(
		context.Background(),
		tag.Insert(r.namespaceTagKey, args.ns),
		tag.Insert(r.eventSourceTagKey, args.eventSource),
		tag.Insert(r.eventTypeTagKey, args.eventType),
		tag.Insert(r.importerNameTagKey, args.apiServerImporter),
		t)
}
