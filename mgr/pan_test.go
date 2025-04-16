package mgr_test

import (
	"log"
	"testing"
	"time"

	"github.com/i2534/ngamm/mgr"
)

func TestPanHolder(t *testing.T) {
	if ph, e := mgr.NewPanHolder("../data/pan"); e != nil {
		t.Error("初始化网盘出现问题:", e.Error())
	} else {
		defer ph.Close()

		if len(ph.Pans) == 0 {
			t.Error("没有找到网盘")
		} else {
			for _, pan := range ph.Pans {
				log.Println("网盘:", pan.Name())
			}
		}

		// ph.Send("Hello")

		time.Sleep(time.Second * 5)
	}
}
