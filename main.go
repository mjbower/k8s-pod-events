package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type oldPod struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
}

type Pod struct {
	Action    string `json:"action"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
}

var podList = make(map[Pod]string)

func main() {

	var kubeconfig *string

	if home := homedir.HomeDir(); home != "[I" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")

	}

	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Panic(err.Error())
	}

	// stop signal for the informer
	stopper := make(chan struct{})
	defer close(stopper)

	factory := informers.NewSharedInformerFactory(clientset, 0)
	podInformer := factory.Core().V1().Pods()
	informer := podInformer.Informer()

	defer runtime.HandleCrash()

	// start informer ->
	go factory.Start(stopper)

	// start to sync and call list
	if !cache.WaitForCacheSync(stopper, informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: onAdd,  // register add eventhandler
		UpdateFunc: onUpdate, //
		DeleteFunc: onDelete,

		// UpdateFunc: func(interface{}, interface{}) { fmt.Println("update not implemented") },
		// DeleteFunc: func(interface{}) { fmt.Println("delete not implemented") },
	})

	// find pods in one ns, or find pods from --all-namespaces
	// lister := podInformer.Lister().Pods("test-ns")

	// pods, err := lister.List(labels.Everything())

	// if err != nil {
	// 	fmt.Println(err)
	// }

	// fmt.Println("pods:", pods)

	<-stopper
}

func onAdd(obj interface{}) {
	mObj, ok := obj.(*corev1.Pod)
	if !ok {
		log.Panic("Not a Pod added")
	}
	pStatus := getPodStatus("NEW",mObj)

	json := createJson("add",mObj.Name,mObj.Namespace,pStatus)
	_ = json	// SEND WEBSOCKET

	//fmt.Printf("Pod added %s\n",mObj.Name)
}

func onUpdate(oldObj interface{},newObj interface{}) {
	oObj, ok := oldObj.(*corev1.Pod)
	nObj, ok := newObj.(*corev1.Pod)
	if !ok {
		log.Panic("Not a Pod added")
	}

	// if we get a pod Running, but with 0/1 , we don't detect that.
	
	// fmt.Printf("Old Pod Updated Name(%s) Phase(%s) Status(%v)\n",oObj.Name,oObj.Status.Phase,oObj.Status.ContainerStatuses)
	// fmt.Printf("New Pod Updated Name(%s) Phase(%s) \n",nObj.Name,nObj.Status.Phase)
	var pStatus string
	pStatus = getPodStatus("OLD",oObj)
	//fmt.Printf("ZZ OLD Status Pod(%s) namespace(%s) Status(%s)\n",oObj.Name,oObj.Namespace,pStatus)
	pStatus = getPodStatus("NEW",nObj)
	json := createJson("add",nObj.Name,nObj.Namespace,pStatus)
	_ = json	// SEND WEBSOCKET
	//fmt.Printf("ZZ NEW Status Pod(%s) namespace(%s) Status(%s)\n",nObj.Name,nObj.Namespace,pStatus)
}

func createJson(action,name,ns,status string) []byte{
	newPod := Pod{ 
		Action: action,
		Name: name, 
		Namespace: ns,
		Status: status, 
	}
	jsonMsg, err := json.Marshal(newPod)
	if err != nil {
		log.Fatalf("Error happened in JSON marshal. Err: %s", err)
	}
	fmt.Printf("\tSent %s\n",jsonMsg)
	return jsonMsg
}

func onDelete(obj interface{}) {
	mObj, ok := obj.(*corev1.Pod)
	if !ok {
		log.Panic("Not a Pod added")
	}

	json := createJson("delete",mObj.Name,mObj.Namespace,"deleted")
	_ = json	// SEND WEBSOCKET
	//fmt.Printf("Pod Deleted Name(%s) Namespace(%s)\n",mObj.Name,mObj.Namespace)
}

var (
	podSuccessConditions = []metav1.TableRowCondition{{Type: metav1.RowCompleted, Status: metav1.ConditionTrue, Reason: string(corev1.PodSucceeded), Message: "The pod has completed successfully."}}
	podFailedConditions  = []metav1.TableRowCondition{{Type: metav1.RowCompleted, Status: metav1.ConditionTrue, Reason: string(corev1.PodFailed), Message: "The pod failed."}}
)

func getPodStatus(someText string, pod *corev1.Pod) string{
	restarts := 0
		//totalContainers := len(pod.Spec.Containers)
		readyContainers := 0

		
		reason := string(pod.Status.Phase)
		if pod.Status.Reason != "" {
			reason = pod.Status.Reason
		}
		//fmt.Printf("XX %s - Pod (start) Name(%s) REASON %s\n",someText, pod.Name,reason)

		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			fmt.Printf("XXXX Pod Succeeded %s\n",podSuccessConditions)
		case corev1.PodFailed:
			fmt.Printf("XXXX Pod Failed %s\n",podFailedConditions)
		}

		initializing := false
		for i := range pod.Status.InitContainerStatuses {
			container := pod.Status.InitContainerStatuses[i]
			restarts += int(container.RestartCount)
			switch {
			case container.State.Terminated != nil && container.State.Terminated.ExitCode == 0:
				continue
			case container.State.Terminated != nil:
				// initialization is failed
				if len(container.State.Terminated.Reason) == 0 {
					if container.State.Terminated.Signal != 0 {
						reason = fmt.Sprintf("XX Init:Signal:%d", container.State.Terminated.Signal)
					} else {
						reason = fmt.Sprintf("XX Init:ExitCode:%d", container.State.Terminated.ExitCode)
					}
				} else {
					reason = "XX Init:" + container.State.Terminated.Reason
				}
				initializing = true
			case container.State.Waiting != nil && len(container.State.Waiting.Reason) > 0 && container.State.Waiting.Reason != "PodInitializing":
				reason = "XX Init:" + container.State.Waiting.Reason
				initializing = true
			default:
				reason = fmt.Sprintf("XX Init:%d/%d\n", i, len(pod.Spec.InitContainers))
				initializing = true
			}
			break
		}
		if !initializing {
			restarts = 0
			hasRunning := false
			for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
				container := pod.Status.ContainerStatuses[i]

				restarts += int(container.RestartCount)
				if container.State.Waiting != nil && container.State.Waiting.Reason != "" {
					reason = container.State.Waiting.Reason
				} else if container.State.Terminated != nil && container.State.Terminated.Reason != "" {
					reason = container.State.Terminated.Reason
				} else if container.State.Terminated != nil && container.State.Terminated.Reason == "" {
					if container.State.Terminated.Signal != 0 {
						reason = fmt.Sprintf("XX Signal:%d\n", container.State.Terminated.Signal)
					} else {
						reason = fmt.Sprintf("XX ExitCode:%d\n", container.State.Terminated.ExitCode)
					}
				} else if container.Ready && container.State.Running != nil {
					hasRunning = true
					readyContainers++
				}
			}

			// change pod status back to "Running" if there is at least one container still reporting as "Running" status
			if reason == "Completed" && hasRunning {
				if hasPodReadyCondition(pod.Status.Conditions) {
					reason = "Running"
				} else {
					reason = "NotReady"
				}
			}
		}

		if pod.DeletionTimestamp != nil {
			reason = "Terminating"
		}
		// SEND ME to WEBSOCKET
		//fmt.Printf("XX %s - Final (finish) Name(%s) REASON %s\n",someText,pod.Name,reason)
		return reason

}

func hasPodReadyCondition(conditions []corev1.PodCondition) bool {
	for _, condition := range conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
