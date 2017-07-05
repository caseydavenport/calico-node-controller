package main

import (
	"flag"
	"fmt"
	"time"

	calicache "github.com/projectcalico/node-controller/pkg/cache"

	"github.com/projectcalico/libcalico-go/lib/api"
	"github.com/projectcalico/libcalico-go/lib/client"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/runtime/schema"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/fields"
	uruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	// "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	// kapi "k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

// CalicoNode runtime.Object representation.
type CalicoNode struct {
	api.Node
}

func (c CalicoNode) GetObjectKind() schema.ObjectKind {
	return CalicoNodeObjectKind{}
}

type CalicoNodeObjectKind struct {
}

func (c CalicoNodeObjectKind) SetGroupVersionKind(kind schema.GroupVersionKind) {}
func (c CalicoNodeObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Node",
	}
}

func main() {
	var kubeconfig string
	var master string

	flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	flag.StringVar(&master, "master", "", "master url")
	flag.Parse()

	// creates the connection
	config, err := clientcmd.BuildConfigFromFlags(master, kubeconfig)
	if err != nil {
		glog.Fatal(err)
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatal(err)
	}

	// create the watcher
	nodeListWatcher := cache.NewListWatchFromClient(clientset.Core().RESTClient(), "nodes", "", fields.Everything())

	// create the workqueue
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	// Bind the workqueue to a cache with the help of an informer. This way we make sure that
	// whenever the cache is updated, the node key is added to the workqueue.
	// Note that when we finally process the item from the workqueue, we might see a newer version
	// of the Node than the version which was responsible for triggering the update.
	indexer, informer := cache.NewIndexerInformer(nodeListWatcher, &v1.Node{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(QueueUpdate{key, false})
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				queue.Add(QueueUpdate{key, false})
			}
		},
		DeleteFunc: func(obj interface{}) {
			// IndexerInformer uses a delta queue, therefore for deletes we have to use this
			// key function.
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(QueueUpdate{key, false})
			}
		},
	}, cache.Indexers{})

	controller := NewController(queue, indexer, informer)

	// Now let's start the Kubernetes controller
	fmt.Println("Starting controller")
	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(1, stop)

	// Wait forever.
	select {}
}

type Controller struct {
	indexer        cache.Indexer
	queue          workqueue.RateLimitingInterface
	informer       cache.Controller
	calicoObjCache calicache.ResourceCache
	calicoClient   *client.Client
}

type QueueUpdate struct {
	Key   string
	Force bool
}

func (c *Controller) Run(threadiness int, stopCh chan struct{}) {
	defer uruntime.HandleCrash()

	// Let the workers stop when we are done
	defer c.queue.ShutDown()
	glog.Info("Starting Node controller")

	go c.informer.Run(stopCh)

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		uruntime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	// Send any required deletes.
	c.populateCalicoCache()
	go c.periodicDatastoreSync()

	// TODO: For now, threadiness MUST be 1 since we don't lock the secondary
	// Calico object cache.  Once we add locking to that cache, we can start multiple
	// worker routines.
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
	glog.Info("Stopping Node controller")
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func (c *Controller) periodicDatastoreSync() {
	fmt.Println("Starting periodic resync thread")
	for {
		fmt.Println("Performing a periodic resync")
		c.performDatastoreSync()
		fmt.Println("Periodic resync done")
		time.Sleep(20 * time.Second)
	}
}

func (c *Controller) performDatastoreSync() {
	// First, let's bring the Calico cache in-sync with what's actually in etcd.
	calicoNodes, err := c.calicoClient.Nodes().List(api.NodeMetadata{})
	if err != nil {
		panic(err)
	}

	// Build a map of existing nodes, plus a map including all keys that exist.
	allKeys := map[string]bool{}
	existing := map[string]interface{}{}
	for _, no := range calicoNodes.Items {
		k := no.Metadata.Name
		existing[k] = no
		allKeys[k] = true
	}

	// Sync the in-memory cache with etcd.  We don't care about entries that exist in
	// etcd but not in our cache - we'll update those anyway.
	for _, k := range c.calicoObjCache.ListKeys() {
		if _, ok := existing[k]; !ok {
			// No longer in etcd - delete it from cache.
			c.calicoObjCache.Delete(k)
		} else {
			// Update cache with data from etcd.
		}
	}

	// Now, send through all existing keys from the Kubernetes API so we can
	// sync them, if needed.
	for _, k := range c.indexer.ListKeys() {
		allKeys[k] = true
	}
	fmt.Printf("Refreshing %d keys in total", len(allKeys))
	for k, _ := range allKeys {
		c.queue.Add(QueueUpdate{k, false})
	}
}

func (c *Controller) populateCalicoCache() {
	// Populate the Calico cache.
	calicoNodes, err := c.calicoClient.Nodes().List(api.NodeMetadata{})
	if err != nil {
		panic(err)
	}
	for _, no := range calicoNodes.Items {
		k := no.Metadata.Name
		c.calicoObjCache.Set(k, no)
	}
}

func (c *Controller) processNextItem() bool {
	// Wait until there is a new item in the working queue
	upd, quit := c.queue.Get()
	if quit {
		return false
	}
	// Tell the queue that we are done with processing this key. This unblocks the key for other workers
	// This allows safe parallel processing because two nodes with the same key are never processed in
	// parallel.
	defer c.queue.Done(upd)

	// Invoke the method containing the business logic
	err := c.syncToCalico(upd.(QueueUpdate))

	// Handle the error if something went wrong during the execution of the business logic
	c.handleErr(err, upd)
	return true
}

// handleErr checks if an error happened and makes sure we will retry later.
func (c *Controller) handleErr(err error, upd interface{}) {
	if err == nil {
		// Forget about the #AddRateLimited history of the key on every successful synchronization.
		// This ensures that future processing of updates for this key is not delayed because of
		// an outdated error history.
		c.queue.Forget(upd)
		return
	}

	// This controller retries 5 times if something goes wrong. After that, it stops trying.
	if c.queue.NumRequeues(upd) < 5 {
		glog.Infof("Error syncing node %v: %v", upd, err)

		// Re-enqueue the key rate limited. Based on the rate limiter on the
		// queue and the re-enqueue history, the key will be processed later again.
		c.queue.AddRateLimited(upd)
		return
	}

	c.queue.Forget(upd)
	// Report to an external entity that, even after several retries, we could not successfully process this key
	uruntime.HandleError(err)
	glog.Infof("Dropping node %q out of the queue: %v", upd, err)
}

// syncToCalico is the business logic of the controller. In this controller it simply prints
// information about the node to stdout. In case an error happened, it has to simply return the error.
// The retry logic should not be part of the business logic.
func (c *Controller) syncToCalico(upd QueueUpdate) error {
	key := upd.Key
	obj, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		glog.Errorf("Fetching object with key %s from store failed with %v", key, err)
		return err
	}

	if !exists {
		fmt.Printf("Node %s does not exist anymore\n", key)

		// Check if it exists in our cache.
		no, ok := c.calicoObjCache.Get(key)
		if ok || upd.Force {
			// If it does, then remove it.
			fmt.Printf("Deleting node %s\n", key)
			c.calicoObjCache.Delete(key)
			return c.calicoClient.Nodes().Delete(no.(api.Node).Metadata)
		}
		// Otherwise, this is a no-op.
		fmt.Printf("No-op delete\n")
		return nil
	} else {
		// Generate the Calico representation of this Node.
		no := api.Node{Metadata: api.NodeMetadata{Name: obj.(*v1.Node).Name}}

		// Only apply an update if it's:
		// - Not in the cache
		// - Different from what's in the cache.
		// - This is a forced udpate.
		if _, exists := c.calicoObjCache.Get(key); !exists || upd.Force {
			fmt.Printf("Sync/Add/Update for Node %s\n", obj.(*v1.Node).Name)
			_, err := c.calicoClient.Nodes().Apply(&no)
			if err != nil {
				return err
			}
			c.calicoObjCache.Set(key, no)
		}
		fmt.Printf("No-op update for %s\n", obj.(*v1.Node).Name)
	}
	return nil
}

func NewController(queue workqueue.RateLimitingInterface, indexer cache.Indexer, informer cache.Controller) *Controller {
	cconfig, err := client.LoadClientConfig("")
	if err != nil {
		panic(err)
	}
	cclient, err := client.New(*cconfig)
	if err != nil {
		panic(err)
	}

	return &Controller{
		informer:       informer,
		indexer:        indexer,
		queue:          queue,
		calicoObjCache: calicache.NewCache(),
		calicoClient:   cclient,
	}
}
