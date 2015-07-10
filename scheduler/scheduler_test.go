package scheduler

import (
	"errors"
	"fmt"
	"testing"
	"time"

	log "github.com/Sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/intelsdi-x/pulse/core"
	"github.com/intelsdi-x/pulse/core/cdata"
	"github.com/intelsdi-x/pulse/core/ctypes"
	"github.com/intelsdi-x/pulse/core/perror"
	"github.com/intelsdi-x/pulse/pkg/schedule"
	"github.com/intelsdi-x/pulse/scheduler/wmap"
)

type mockMetricManager struct {
	failValidatingMetrics      bool
	failValidatingMetricsAfter int
	failuredSoFar              int
	acceptedContentTypes       map[string][]string
	returnedContentTypes       map[string][]string
}

func (m *mockMetricManager) lazyContentType(key string) {
	if m.acceptedContentTypes == nil {
		m.acceptedContentTypes = make(map[string][]string)
	}
	if m.returnedContentTypes == nil {
		m.returnedContentTypes = make(map[string][]string)
	}
	if m.acceptedContentTypes[key] == nil {
		m.acceptedContentTypes[key] = []string{}
	}
	if m.returnedContentTypes[key] == nil {
		m.returnedContentTypes[key] = []string{}
	}
}

// Used to mock type from plugin
func (m *mockMetricManager) setAcceptedContentType(n string, t core.PluginType, v int, s []string) {
	key := fmt.Sprintf("%s:%d:%d", n, t, v)
	m.lazyContentType(key)
	m.acceptedContentTypes[key] = s
}

func (m *mockMetricManager) setReturnedContentType(n string, t core.PluginType, v int, s []string) {
	key := fmt.Sprintf("%s:%d:%d", n, t, v)
	m.lazyContentType(key)
	m.returnedContentTypes[key] = s
}

func (m *mockMetricManager) GetPluginContentTypes(n string, t core.PluginType, v int) ([]string, []string, error) {
	key := fmt.Sprintf("%s:%d:%d", n, t, v)
	m.lazyContentType(key)

	return m.acceptedContentTypes[key], m.returnedContentTypes[key], nil
}

func (m *mockMetricManager) CollectMetrics([]core.Metric, time.Time) ([]core.Metric, []error) {
	return nil, nil
}

func (m *mockMetricManager) PublishMetrics(contentType string, content []byte, pluginName string, pluginVersion int, config map[string]ctypes.ConfigValue) []error {
	return nil
}

func (m *mockMetricManager) ProcessMetrics(contentType string, content []byte, pluginName string, pluginVersion int, config map[string]ctypes.ConfigValue) (string, []byte, []error) {
	return "", nil, nil
}

func (m *mockMetricManager) ValidateDeps(mts []core.Metric, prs []core.SubscribedPlugin) []perror.PulseError {
	if m.failValidatingMetrics {
		return []perror.PulseError{
			perror.New(errors.New("metric validation error")),
		}
	}
	return nil
}
func (m *mockMetricManager) SubscribeDeps(taskId uint64, mts []core.Metric, prs []core.Plugin) []perror.PulseError {
	return []perror.PulseError{
		perror.New(errors.New("metric validation error")),
	}
}

func (m *mockMetricManager) UnsubscribeDeps(taskId uint64, mts []core.Metric, prs []core.Plugin) []perror.PulseError {
	return nil
}

type mockMetricManagerError struct {
	errs []error
}

type mockMetricType struct {
	version            int
	namespace          []string
	lastAdvertisedTime time.Time
	config             *cdata.ConfigDataNode
}

func (m mockMetricType) Version() int {
	return m.version
}

func (m mockMetricType) Namespace() []string {
	return m.namespace
}

func (m mockMetricType) LastAdvertisedTime() time.Time {
	return m.lastAdvertisedTime
}

func (m mockMetricType) Config() *cdata.ConfigDataNode {
	return m.config
}

func (m mockMetricType) Data() interface{} {
	return nil
}

type mockScheduleResponse struct {
}

func (m mockScheduleResponse) state() schedule.ScheduleState {
	return schedule.Active
}

func (m mockScheduleResponse) err() error {
	return nil
}

func (m mockScheduleResponse) missedIntervals() uint {
	return 0
}

func TestScheduler(t *testing.T) {
	log.SetLevel(log.FatalLevel)
	Convey("NewTask", t, func() {
		c := new(mockMetricManager)
		c.setAcceptedContentType("machine", core.ProcessorPluginType, 1, []string{"pulse.*", "pulse.gob", "foo.bar"})
		c.setReturnedContentType("machine", core.ProcessorPluginType, 1, []string{"pulse.gob"})
		c.setAcceptedContentType("rmq", core.PublisherPluginType, -1, []string{"pulse.json", "pulse.gob"})
		c.setAcceptedContentType("file", core.PublisherPluginType, -1, []string{"pulse.json"})
		s := New()
		s.SetMetricManager(c)
		w := wmap.NewWorkflowMap()
		// Collection node
		w.CollectNode.AddMetric("/foo/bar", 1)
		w.CollectNode.AddMetric("/foo/baz", 2)
		w.CollectNode.AddConfigItem("/foo/bar", "username", "root")
		w.CollectNode.AddConfigItem("/foo/bar", "port", 8080)
		w.CollectNode.AddConfigItem("/foo/bar", "ratio", 0.32)
		w.CollectNode.AddConfigItem("/foo/bar", "yesorno", true)

		// Add a process node
		pr1 := wmap.NewProcessNode("machine", 1)
		pr1.AddConfigItem("username", "wat")
		pr1.AddConfigItem("howmuch", 9999)

		// Add a process node
		pr12 := wmap.NewProcessNode("machine", 1)
		pr12.AddConfigItem("username", "wat2")
		pr12.AddConfigItem("howmuch", 99992)

		// Publish node for our process node
		pu1 := wmap.NewPublishNode("rmq", -1)
		pu1.AddConfigItem("birthplace", "dallas")
		pu1.AddConfigItem("monies", 2)

		// Publish node direct to collection
		pu2 := wmap.NewPublishNode("file", -1)
		pu2.AddConfigItem("color", "brown")
		pu2.AddConfigItem("purpose", 42)

		pr12.Add(pu2)
		pr1.Add(pr12)
		w.CollectNode.Add(pr1)
		w.CollectNode.Add(pu1)

		e := s.Start()
		So(e, ShouldBeNil)
		t, te := s.CreateTask(schedule.NewSimpleSchedule(time.Second*1), w, false)
		So(te.Errors(), ShouldBeEmpty)

		for _, i := range t.(*task).workflow.processNodes {
			testInboundContentType(i)
		}
		for _, i := range t.(*task).workflow.publishNodes {
			testInboundContentType(i)
		}
		So(t.(*task).workflow.processNodes[0].ProcessNodes[0].PublishNodes[0].InboundContentType, ShouldEqual, "pulse.json")

		Convey("returns errors when metrics do not validate", func() {
			c.failValidatingMetrics = true
			c.failValidatingMetricsAfter = 1
			_, err := s.CreateTask(schedule.NewSimpleSchedule(time.Second*1), w, false)
			So(err, ShouldNotBeNil)
			fmt.Printf("%d", len(err.Errors()))
			So(len(err.Errors()), ShouldBeGreaterThan, 0)
			So(err.Errors()[0], ShouldResemble, perror.New(errors.New("metric validation error")))

		})

		Convey("returns an error when scheduler started and MetricManager is not set", func() {
			s1 := New()
			err := s1.Start()
			So(err, ShouldNotBeNil)
			fmt.Printf("%v", err)
			So(err, ShouldResemble, ErrMetricManagerNotSet)

		})

		Convey("returns an error when wrong namespace is given wo workflowmap ", func() {
			w.CollectNode.AddMetric("****/&&&", 3)
			w.CollectNode.AddConfigItem("****/&&&", "username", "user")
			_, err := s.CreateTask(schedule.NewSimpleSchedule(time.Second*1), w, false)

			So(len(err.Errors()), ShouldBeGreaterThan, 0)

		})

		// TODO NICK
		Convey("returns an error when a schedule does not validate", func() {
			s1 := New()
			s1.Start()
			_, err := s1.CreateTask(schedule.NewSimpleSchedule(time.Second*1), w, false)
			So(err, ShouldNotBeNil)
			So(len(err.Errors()), ShouldBeGreaterThan, 0)
			So(err.Errors()[0], ShouldResemble, perror.New(ErrSchedulerNotStarted))
			s1.metricManager = c
			s1.Start()
			_, err1 := s1.CreateTask(schedule.NewSimpleSchedule(time.Second*0), w, false)
			So(err1.Errors()[0].Error(), ShouldResemble, "Interval must be greater than 0")

		})

		// 		// TODO NICK
		Convey("create a task", func() {

			tsk, err := s.CreateTask(schedule.NewSimpleSchedule(time.Second*5), w, false)
			So(len(err.Errors()), ShouldEqual, 0)
			So(tsk, ShouldNotBeNil)
			So(tsk.(*task).deadlineDuration, ShouldResemble, DefaultDeadlineDuration)
			So(len(s.GetTasks()), ShouldEqual, 2)
			Convey("error when attempting to add duplicate task", func() {
				err := s.tasks.add(tsk.(*task))
				So(err, ShouldNotBeNil)

			})
			Convey("get created task", func() {
				t, err := s.GetTask(tsk.ID())
				So(err, ShouldBeNil)
				So(t, ShouldEqual, tsk)
			})
			Convey("error when attempting to get a task that doesn't exist", func() {
				t, err := s.GetTask(uint64(1234))
				So(err, ShouldNotBeNil)
				So(t, ShouldBeNil)
			})
		})

		// 		// // TODO NICK
		Convey("returns a task with a 6 second deadline duration", func() {
			tsk, err := s.CreateTask(schedule.NewSimpleSchedule(time.Second*6), w, false, core.TaskDeadlineDuration(6*time.Second))
			So(len(err.Errors()), ShouldEqual, 0)
			So(tsk.(*task).deadlineDuration, ShouldResemble, time.Duration(6*time.Second))
			prev := tsk.(*task).Option(core.TaskDeadlineDuration(1 * time.Second))
			So(tsk.(*task).deadlineDuration, ShouldResemble, time.Duration(1*time.Second))
			tsk.(*task).Option(prev)
			So(tsk.(*task).deadlineDuration, ShouldResemble, time.Duration(6*time.Second))
		})
	})
	Convey("Stop()", t, func() {
		Convey("Should set scheduler state to SchedulerStopped", func() {
			scheduler := New()
			c := new(mockMetricManager)
			scheduler.metricManager = c
			scheduler.Start()
			scheduler.Stop()
			So(scheduler.state, ShouldEqual, schedulerStopped)
		})
	})
	Convey("SetMetricManager()", t, func() {
		Convey("Should set metricManager for scheduler", func() {
			scheduler := New()
			c := new(mockMetricManager)
			scheduler.SetMetricManager(c)
			So(scheduler.metricManager, ShouldEqual, c)
		})
	})

}

func testInboundContentType(node interface{}) {
	switch t := node.(type) {
	case *processNode:
		fmt.Printf("testing content type for pr plugin %s %d/n", t.Name(), t.Version())
		So(t.InboundContentType, ShouldNotEqual, "")
		for _, i := range t.ProcessNodes {
			testInboundContentType(i)
		}
	case *publishNode:
		fmt.Printf("testing content type for pu plugin %s %d/n", t.Name(), t.Version())
		So(t.InboundContentType, ShouldNotEqual, "")
	}
}
