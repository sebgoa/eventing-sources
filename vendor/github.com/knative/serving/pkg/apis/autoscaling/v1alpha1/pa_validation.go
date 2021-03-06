/*
Copyright 2018 The Knative Authors

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

package v1alpha1

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/kmp"
	"github.com/knative/serving/pkg/apis/autoscaling"
	servingv1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/api/equality"
)

func (pa *PodAutoscaler) Validate(ctx context.Context) *apis.FieldError {
	return servingv1alpha1.ValidateObjectMetadata(pa.GetObjectMeta()).
		ViaField("metadata").
		Also(pa.Spec.Validate(ctx).ViaField("spec")).
		Also(pa.validateMetric())
}

// Validate validates PodAutoscaler Spec.
func (rs *PodAutoscalerSpec) Validate(ctx context.Context) *apis.FieldError {
	if equality.Semantic.DeepEqual(rs, &PodAutoscalerSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	errs := validateReference(rs.ScaleTargetRef).ViaField("scaleTargetRef")
	if rs.ServiceName == "" {
		errs = errs.Also(apis.ErrMissingField("serviceName"))
	}
	if err := rs.ConcurrencyModel.Validate(ctx); err != nil {
		errs = errs.Also(err.ViaField("concurrencyModel"))
	} else if err := servingv1alpha1.ValidateContainerConcurrency(rs.ContainerConcurrency, rs.ConcurrencyModel); err != nil {
		errs = errs.Also(err)
	}
	return errs.Also(validateSKSFields(rs))
}

func validateSKSFields(rs *PodAutoscalerSpec) *apis.FieldError {
	var all *apis.FieldError
	// TODO(vagababov) stop permitting empty protocol type, once SKS controller is live.
	if string(rs.ProtocolType) != "" {
		all = all.Also(rs.ProtocolType.Validate()).ViaField("protocolType")
	}
	return all
}

func validateReference(ref autoscalingv1.CrossVersionObjectReference) *apis.FieldError {
	if equality.Semantic.DeepEqual(ref, autoscalingv1.CrossVersionObjectReference{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var errs *apis.FieldError
	if ref.Kind == "" {
		errs = errs.Also(apis.ErrMissingField("kind"))
	}
	if ref.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	}
	if ref.APIVersion == "" {
		errs = errs.Also(apis.ErrMissingField("apiVersion"))
	}
	return errs
}

func (pa *PodAutoscaler) validateMetric() *apis.FieldError {
	if metric, ok := pa.Annotations[autoscaling.MetricAnnotationKey]; ok {
		switch pa.Class() {
		case autoscaling.KPA:
			switch metric {
			case autoscaling.Concurrency:
				return nil
			}
		case autoscaling.HPA:
			switch metric {
			case autoscaling.CPU:
				return nil
			}
			// TODO: implement OPS autoscaling.
		default:
			// Leave other classes of PodAutoscaler alone.
			return nil
		}
		return &apis.FieldError{
			Message: fmt.Sprintf("Unsupported metric %q for PodAutoscaler class %q",
				metric, pa.Class()),
			Paths: []string{"annotations[autoscaling.knative.dev/metric]"},
		}
	}
	return nil
}

// CheckImmutableFields checks the immutability of the PodAutoscaler.
func (current *PodAutoscaler) CheckImmutableFields(ctx context.Context, og apis.Immutable) *apis.FieldError {
	original, ok := og.(*PodAutoscaler)
	if !ok {
		return &apis.FieldError{Message: "The provided original was not a PodAutoscaler"}
	}

	// TODO(vagababov): remove after 0.6. This is temporary plug for backwards compatibility.
	opt := cmp.FilterPath(
		func(p cmp.Path) bool {
			return p.String() == "ProtocolType"
		},
		cmp.Ignore(),
	)
	if diff, err := kmp.SafeDiff(original.Spec, current.Spec, opt); err != nil {
		return &apis.FieldError{
			Message: "Failed to diff PodAutoscaler",
			Paths:   []string{"spec"},
			Details: err.Error(),
		}
	} else if diff != "" {
		return &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: diff,
		}
	}
	// Verify the PA class does not change.
	// For backward compatibility, we allow a new class where there was none before.
	if oldClass, ok := original.Annotations[autoscaling.ClassAnnotationKey]; ok {
		if newClass, ok := current.Annotations[autoscaling.ClassAnnotationKey]; !ok || oldClass != newClass {
			return &apis.FieldError{
				Message: fmt.Sprintf("Immutable class annotation changed (-%q +%q)", oldClass, newClass),
				Paths:   []string{"annotations[autoscaling.knative.dev/class]"},
			}
		}
	}
	return nil
}
