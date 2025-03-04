package slo

import (
	"fmt"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// QueryTotal returns a PromQL query to get the total amount of requests served during the window.
func (o Objective) QueryTotal(window model.Duration) string {
	expr, err := parser.ParseExpr(`sum by (grouping) (increase(metric{}[1s]))`)
	if err != nil {
		return ""
	}

	var metric string
	var matchers []*labels.Matcher
	var grouping []string

	if o.Indicator.Ratio != nil && o.Indicator.Ratio.Total.Name != "" {
		metric = o.Indicator.Ratio.Total.Name
		matchers = o.Indicator.Ratio.Total.LabelMatchers
		grouping = o.Indicator.Ratio.Grouping
	}
	if o.Indicator.Latency != nil && o.Indicator.Latency.Total.Name != "" {
		metric = o.Indicator.Latency.Total.Name
		matchers = o.Indicator.Latency.Total.LabelMatchers
		grouping = o.Indicator.Latency.Grouping
	}

	objectiveReplacer{
		metric:   metric,
		matchers: matchers,
		grouping: grouping,
		window:   time.Duration(window),
	}.replace(expr)

	return expr.String()
}

// QueryErrors returns a PromQL query to get the amount of request errors during the window.
func (o Objective) QueryErrors(window model.Duration) string {
	if o.Indicator.Ratio != nil && o.Indicator.Ratio.Total.Name != "" {
		expr, err := parser.ParseExpr(`sum by(grouping) (increase(metric{}[1s]))`)
		if err != nil {
			return err.Error()
		}

		objectiveReplacer{
			metric:   o.Indicator.Ratio.Errors.Name,
			matchers: o.Indicator.Ratio.Errors.LabelMatchers,
			grouping: o.Indicator.Ratio.Grouping,
			window:   time.Duration(window),
		}.replace(expr)

		return expr.String()
	}

	if o.Indicator.Latency != nil && o.Indicator.Latency.Total.Name != "" {
		expr, err := parser.ParseExpr(`sum by(grouping) (increase(metric{matchers="total"}[1s])) - sum by(grouping) (increase(errorMetric{matchers="errors"}[1s]))`)
		if err != nil {
			return err.Error()
		}

		objectiveReplacer{
			metric:        o.Indicator.Latency.Total.Name,
			matchers:      o.Indicator.Latency.Total.LabelMatchers,
			errorMetric:   o.Indicator.Latency.Success.Name,
			errorMatchers: o.Indicator.Latency.Success.LabelMatchers,
			grouping:      o.Indicator.Latency.Grouping,
			window:        time.Duration(window),
		}.replace(expr)

		return expr.String()
	}

	return ""
}

func (o Objective) QueryErrorBudget() string {
	if o.Indicator.Ratio != nil && o.Indicator.Ratio.Total.Name != "" {
		expr, err := parser.ParseExpr(`
(
  (1 - 0.696969)
  -
  (
    sum by(job, handler) (increase(errorMetric{matchers="errors"}[1s]) or vector(0))
    /
    sum by(job, handler) (increase(metric{matchers="total"}[1s]))
  )
)
/
(1 - 0.696969)
`)
		if err != nil {
			return ""
		}

		objectiveReplacer{
			metric:        o.Indicator.Ratio.Total.Name,
			matchers:      o.Indicator.Ratio.Total.LabelMatchers,
			errorMetric:   o.Indicator.Ratio.Errors.Name,
			errorMatchers: o.Indicator.Ratio.Errors.LabelMatchers,
			grouping:      o.Indicator.Ratio.Grouping,
			window:        time.Duration(o.Window),
			target:        o.Target,
		}.replace(expr)

		return expr.String()
	}

	if o.Indicator.Latency != nil && o.Indicator.Latency.Total.Name != "" {
		expr, err := parser.ParseExpr(`
(
  (1 - 0.696969)
  -
  (
    1 -
    sum(increase(errorMetric{matchers="errors"}[1s]) or vector(0))
    /
    sum(increase(metric{matchers="total"}[1s]))
  )
)
/
(1 - 0.696969)
`)
		if err != nil {
			return ""
		}

		objectiveReplacer{
			metric:        o.Indicator.Latency.Total.Name,
			matchers:      o.Indicator.Latency.Total.LabelMatchers,
			errorMetric:   o.Indicator.Latency.Success.Name,
			errorMatchers: o.Indicator.Latency.Success.LabelMatchers,
			grouping:      o.Indicator.Latency.Grouping,
			window:        time.Duration(o.Window),
			target:        o.Target,
		}.replace(expr)

		return expr.String()
	}

	return ""
}

type objectiveReplacer struct {
	metric        string
	matchers      []*labels.Matcher
	errorMetric   string
	errorMatchers []*labels.Matcher
	grouping      []string
	window        time.Duration
	target        float64
}

func (r objectiveReplacer) replace(node parser.Node) {
	switch n := node.(type) {
	case *parser.AggregateExpr:
		if len(n.Grouping) > 0 {
			n.Grouping = r.grouping
		}
		r.replace(n.Expr)
	case *parser.Call:
		r.replace(n.Args)
	case parser.Expressions:
		for _, expr := range n {
			r.replace(expr)
		}
	case *parser.MatrixSelector:
		n.Range = r.window
		r.replace(n.VectorSelector)
	case *parser.VectorSelector:
		if n.Name == "errorMetric" {
			n.Name = r.errorMetric
		} else {
			n.Name = r.metric
		}
		if len(n.LabelMatchers) > 1 {
			for _, m := range n.LabelMatchers {
				if m.Name == "matchers" {
					if m.Value == "errors" {
						n.LabelMatchers = r.errorMatchers
					} else {
						n.LabelMatchers = r.matchers
					}
				}
			}
		} else {
			n.LabelMatchers = r.matchers
		}
	case *parser.BinaryExpr:
		r.replace(n.LHS)
		r.replace(n.RHS)
	case *parser.ParenExpr:
		r.replace(n.Expr)
	case *parser.NumberLiteral:
		if n.Val == 0.696969 {
			n.Val = r.target
		}
	default:
		panic(fmt.Sprintf("no support for type %T", n))
	}
}

func (o Objective) RequestRange(timerange time.Duration) string {
	if o.Indicator.Ratio != nil && o.Indicator.Ratio.Total.Name != "" {
		expr, err := parser.ParseExpr(`sum by(group) (rate(metric{}[1s])) > 0`)
		if err != nil {
			return err.Error()
		}

		objectiveReplacer{
			metric:   o.Indicator.Ratio.Total.Name,
			matchers: o.Indicator.Ratio.Total.LabelMatchers,
			grouping: groupingLabels(
				o.Indicator.Ratio.Errors.LabelMatchers,
				o.Indicator.Ratio.Total.LabelMatchers,
			),
			window: timerange,
			target: o.Target,
		}.replace(expr)

		return expr.String()
	}
	if o.Indicator.Latency != nil && o.Indicator.Latency.Total.Name != "" {
		expr, err := parser.ParseExpr(`sum(rate(metric{}[1s]))`)
		if err != nil {
			return err.Error()
		}

		objectiveReplacer{
			metric:        o.Indicator.Latency.Total.Name,
			matchers:      o.Indicator.Latency.Total.LabelMatchers,
			errorMetric:   o.Indicator.Latency.Success.Name,
			errorMatchers: o.Indicator.Latency.Success.LabelMatchers,
			window:        timerange,
		}.replace(expr)

		return expr.String()
	}
	return ""
}

func (o Objective) ErrorsRange(timerange time.Duration) string {
	if o.Indicator.Ratio != nil && o.Indicator.Ratio.Total.Name != "" {
		expr, err := parser.ParseExpr(`sum by(group) (rate(errorMetric{matchers="errors"}[1s])) / scalar(sum(rate(metric{matchers="total"}[1s])))`)
		if err != nil {
			return err.Error()
		}

		objectiveReplacer{
			metric:        o.Indicator.Ratio.Total.Name,
			matchers:      o.Indicator.Ratio.Total.LabelMatchers,
			errorMetric:   o.Indicator.Ratio.Errors.Name,
			errorMatchers: o.Indicator.Ratio.Errors.LabelMatchers,
			grouping: groupingLabels(
				o.Indicator.Ratio.Errors.LabelMatchers,
				o.Indicator.Ratio.Total.LabelMatchers,
			),
			window: timerange,
		}.replace(expr)

		return expr.String()
	}
	if o.Indicator.Latency != nil && o.Indicator.Latency.Total.Name != "" {
		expr, err := parser.ParseExpr(`sum(rate(metric{matchers="total"}[1s])) -  sum(rate(errorMetric{matchers="errors"}[1s]))`)
		if err != nil {
			return err.Error()
		}

		objectiveReplacer{
			metric:        o.Indicator.Latency.Total.Name,
			matchers:      o.Indicator.Latency.Total.LabelMatchers,
			errorMetric:   o.Indicator.Latency.Success.Name,
			errorMatchers: o.Indicator.Latency.Success.LabelMatchers,
			window:        timerange,
		}.replace(expr)

		return expr.String()
	}
	return ""
}

func groupingLabels(errorMatchers, totalMatchers []*labels.Matcher) []string {
	groupingLabels := map[string]struct{}{}
	for _, m := range errorMatchers {
		groupingLabels[m.Name] = struct{}{}
	}
	for _, m := range totalMatchers {
		delete(groupingLabels, m.Name)
	}
	grouping := make([]string, 0, len(groupingLabels))
	for l := range groupingLabels {
		grouping = append(grouping, l)
	}
	return grouping
}
