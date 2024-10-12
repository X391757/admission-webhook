package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	admission "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var replicaCounter int
var mutex sync.Mutex

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func handleMutate(w http.ResponseWriter, r *http.Request) {
	mutex.Lock() // 保证计数器的线程安全
	defer mutex.Unlock()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	var admissionReview admission.AdmissionReview
	if _, _, err := deserializer.Decode(body, nil, &admissionReview); err != nil {
		http.Error(w, fmt.Sprintf("Error decoding admission review: %v", err), http.StatusBadRequest)
		return
	}

	var pod corev1.Pod
	if err := json.Unmarshal(admissionReview.Request.Object.Raw, &pod); err != nil {
		http.Error(w, fmt.Sprintf("Error unmarshaling pod: %v", err), http.StatusBadRequest)
		return
	}

	var patches []patchOperation
	if replicaCounter == 0 {
		patches = []patchOperation{
			{
				Op:   "add",
				Path: "/spec/affinity",
				Value: corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "node.kubernetes.io/capacity",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"on-demand"},
										},
									},
								},
							},
						},
					},
				},
			},
		}
	} else {
		patches = []patchOperation{
			{
				Op:   "add",
				Path: "/spec/affinity",
				Value: corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
							{
								Weight: 10,
								Preference: corev1.NodeSelectorTerm{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "node.kubernetes.io/capacity",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"spot"},
										},
									},
								},
							},
						},
					},
				},
			},
		}
	}

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error marshaling JSON patch: %v", err), http.StatusInternalServerError)
		return
	}

	admissionResponse := admission.AdmissionResponse{
		UID:     admissionReview.Request.UID,
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *admission.PatchType {
			pt := admission.PatchTypeJSONPatch
			return &pt
		}(),
	}

	admissionReview.Response = &admissionResponse
	resp, err := json.Marshal(admissionReview)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error marshaling admission review response: %v", err), http.StatusInternalServerError)
		return
	}
	replicaCounter++
	fmt.Println("counter changed")
	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
	fmt.Println("finished response")
}

func main() {
	replicaCounter = 0
	http.HandleFunc("/mutate", handleMutate)
	fmt.Println("Starting webhook server on :8443")
	if err := http.ListenAndServeTLS(":8443", "/etc/webhook/certs/tls.crt", "/etc/webhook/certs/tls.key", nil); err != nil {
		panic(err)
	}
}
