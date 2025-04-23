package push

import (
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testEmail = "xxx@gmail.com"

var UTC8 = time.FixedZone("Asia/Shanghai", 8*60*60)

// go test -v -count=1 . -run SendRegularEmail
func TestSendRegularEmail(t *testing.T) {
	client := NewEngageLabEmailClient("")
	rsp, err := client.SendRegular(
		"test@bitmail.bit.com",
		[]string{testEmail},
		"Test Email Push",
		"",
		"Hello from bit 💖. Now "+time.Now().In(UTC8).Format(time.RFC3339),
		uuid.New().String(),
	)
	log.Printf("rsp: %v, err: %v", rsp.Json(), err)
}

// go test -v -count=1 . -run SendTemplateEmail
func TestSendTemplateEmail(t *testing.T) {
	client := NewEngageLabEmailClient("")
	rsp, err := client.SendTemplate(
		"test@bitmail.bit.com",
		[]string{testEmail},
		"Test Email Push, %uid%",
		"dev_template_1", // EngageLab web admin: Email >> Send Related >> Template
		map[string][]any{
			"uid":         {314159},
			"name":        {"Amos"},
			"email":       {testEmail},
			"active_code": {123456},
		},
		uuid.New().String(),
	)
	log.Printf("rsp: %v, err: %v", rsp.Json(), err)
}

// go test -v -count=1 ./common/push/ -run GetTemplates
func Local_TestGetTemplates(t *testing.T) {
	client := NewEngageLabEmailClient("")
	xs, err := client.GetTemplates()
	if err != nil {
		log.Printf("GetTemplates failed: %v", err)
	} else {
		for _, x := range xs {
			log.Printf("%s: %s", x.TemplateInvokeName, x.Name)
		}
	}
}
