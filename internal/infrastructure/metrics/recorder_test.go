package metrics

import (
	"testing"
	"time"
)

func TestNoOpMetricsSatisfiesInterface(t *testing.T) {
	var recorder MetricsRecorder = NoOpMetrics{}
	// Call all interface methods - should not panic
	recorder.RecordLoginAttempt(true, "password")
	recorder.RecordUserRegistration()
	recorder.RecordNotificationSent("email", true)
	recorder.RecordBlogPostCreated("draft")
	recorder.RecordBlogPostPublished()
	recorder.RecordBlogCommentCreated("reply")
	recorder.RecordBlogLikeToggled("like")
	recorder.RecordBlogViewRecorded()
	recorder.RecordBlogShareRecorded("twitter")
	recorder.RecordDBQuery("select", "users", true, 10*time.Millisecond)
	recorder.UpdateDBConnections(10, 5)
	recorder.RecordCacheHit()
	recorder.RecordCacheMiss()
	recorder.RecordMQMessagePublished("exchange", "key", true)
	recorder.RecordMQMessageConsumed("queue", true)
	recorder.UpdateMQMetrics(0, 0, true)
}
