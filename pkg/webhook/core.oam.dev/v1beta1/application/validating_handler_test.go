/*
Copyright 2021 The KubeVela Authors.

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

package application

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ = Describe("Test Application Validator", func() {
	BeforeEach(func() {
		handler = &ValidatingHandler{
			Client:  k8sClient,
			Decoder: decoder,
		}
	})

	It("Test Application Validator [bad request]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object:    runtime.RawExtension{Raw: []byte("bad request")},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test Application Validator [Allow]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1",
"kind":"Application",
"metadata":{"name":"application-sample"},
"spec":{"components":[{"type":"myweb","properties":{"cmd":["sleep","1000"],"image":"busybox"},
"traits":[{"type":"scaler","properties":{"replicas":10}}],"type":"worker"}]}}
`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeTrue())
	})

	It("Test Application Validator [Error]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`{"apiVersion":"core.oam.dev/v1beta1",
"kind":"Application",
"metadata":{"name":"application-sample"},
"spec":{"components":[{"type":"myweb","properties":{"cmd":["sleep","1000"],"image":"busybox"},
"traits":[{"type":"scaler","properties":{"replicas":10}}],"type":"worker1"}]}}`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test Application Validator Forbid rollout annotation", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Update,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1",
"kind":"Application",
"metadata":{"name":"application-sample", "annotations": {"app.oam.dev/rollout" : "true"},}
"spec":{"components":[{"type":"myweb","properties":{"cmd":["sleep","1000"],"image":"busybox"},
"traits":[{"type":"scaler","properties":{"replicas":10}}],"type":"worker"}]}}
`),
				},
				OldObject: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1",
"kind":"Application",
"metadata":{"name":"application-sample"},
"spec":{"components":[{"type":"myweb","properties":{"cmd":["sleep","1000"],"image":"busybox"},
"traits":[{"type":"scaler","properties":{"replicas":10}}],"type":"worker"}]}}
`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test Application Validator workflow step name duplicate [error]", func() {
		By("test duplicated step name in workflow")
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1","kind":"Application","metadata":{"name":"workflow-duplicate","namespace":"default"},"spec":{"components":[{"name":"comp","type":"worker","properties":{"image":"crccheck/hello-world"}}],"workflow":{"steps":[{"name":"suspend","type":"suspend"},{"name":"suspend","type":"suspend"}]}}}
`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())

		By("test duplicated sub step name in workflow")
		req = admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1","kind":"Application","metadata":{"name":"workflow-duplicate","namespace":"default"},"spec":{"components":[{"name":"comp","type":"worker","properties":{"image":"crccheck/hello-world"}}],"workflow":{"steps":[{"name":"group","type":"step-group","subSteps":[{"name":"sub","type":"suspend"},{"name":"sub","type":"suspend"}]}]}}}
`),
				},
			},
		}
		resp = handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())

		By("test duplicated sub and parent step name in workflow")
		req = admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1","kind":"Application","metadata":{"name":"workflow-duplicate","namespace":"default"},"spec":{"components":[{"name":"comp","type":"worker","properties":{"image":"crccheck/hello-world"}}],"workflow":{"steps":[{"name":"group","type":"step-group","subSteps":[{"name":"group","type":"suspend"},{"name":"sub","type":"suspend"}]}]}}}
`),
				},
			},
		}
		resp = handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test Application Validator workflow step invalid timeout [error]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1","kind":"Application","metadata":{"name":"workflow-timeout","namespace":"default"},"spec":{"components":[{"name":"comp","type":"worker","properties":{"image":"crccheck/hello-world"}}],"workflow":{"steps":[{"name":"group","type":"suspend","timeout":"test"}]}}}
`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test Application Validator workflow step invalid timeout [allow]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1","kind":"Application","metadata":{"name":"workflow-timeout","namespace":"default"},"spec":{"components":[{"name":"comp","type":"worker","properties":{"image":"crccheck/hello-world"}}],"workflow":{"steps":[{"name":"group","type":"suspend","timeout":"1s"}]}}}
`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeTrue())
	})

	It("Test Application with empty policy", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1beta1", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"kind":"Application","metadata":{"name":"app-with-empty-policy-webhook-test", "namespace":"default"},
"spec":{"components":[],"policies":[{"name":"2345","type":"garbage-collect","properties":null}]}}
`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test Application with PublishVersion and Autoupdate annotations", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1","kind":"Application","metadata":{"name":"workflow-timeout","namespace":"default","annotations":{"app.oam.dev/publishVersion":"v1.0.0","app.oam.dev/autoUpdate":"true"}},"spec":{"components":[{"name":"comp","type":"worker","properties":{"image":"crccheck/hello-world"}}],"workflow":{"steps":[{"name":"group","type":"suspend","timeout":"1s"}]}}}
`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test Application Publishversion Annotation", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1","kind":"Application","metadata":{"name":"workflow-timeout","namespace":"default","annotations":{"app.oam.dev/publishVersion":"v1.0.0"}},"spec":{"components":[{"name":"comp","type":"worker","properties":{"image":"crccheck/hello-world"}}],"workflow":{"steps":[{"name":"group","type":"suspend","timeout":"1s"}]}}}
`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeTrue())
	})

	It("Test Application Autoupdate Annotation", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha2", Resource: "applications"},
				Object: runtime.RawExtension{
					Raw: []byte(`
{"apiVersion":"core.oam.dev/v1beta1","kind":"Application","metadata":{"name":"workflow-timeout","namespace":"default","annotations":{"app.oam.dev/autoUpdate":"true"}},"spec":{"components":[{"name":"comp","type":"worker","properties":{"image":"crccheck/hello-world"}}],"workflow":{"steps":[{"name":"group","type":"suspend","timeout":"1s"}]}}}
`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeTrue())
	})
})
