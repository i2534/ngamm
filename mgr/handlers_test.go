package mgr

import (
	"testing"

	"github.com/go-playground/assert/v2"
	"github.com/robfig/cron/v3"
)

func TestCron(t *testing.T) {
	_, e := cron.ParseStandard("@every 1h")
	assert.Equal(t, e, nil)

	_, e = cron.ParseStandard("")
	assert.NotEqual(t, e, nil)

	_, e = cron.ParseStandard("@every 1s")
	assert.Equal(t, e, nil)

	_, e = cron.ParseStandard("xxx")
	assert.NotEqual(t, e, nil)

	_, e = cron.ParseStandard("* * * * ?")
	assert.Equal(t, e, nil)

	_, e = cron.ParseStandard("* * * * * ?")
	assert.NotEqual(t, e, nil)
}
