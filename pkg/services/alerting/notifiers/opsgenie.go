package notifiers

import (
	"fmt"
	"strconv"

	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/components/simplejson"
	"github.com/grafana/grafana/pkg/log"
	"github.com/grafana/grafana/pkg/metrics"
	m "github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/alerting"
)

func init() {
	alerting.RegisterNotifier("opsgenie", NewOpsGenieNotifier)
}

var (
	opsgenieCreateAlertURL string = "https://api.opsgenie.com/v1/json/alert"
	opsgenieCloseAlertURL  string = "https://api.opsgenie.com/v1/json/alert/close"
)

func NewOpsGenieNotifier(model *m.AlertNotification) (alerting.Notifier, error) {
	autoClose := model.Settings.Get("autoClose").MustBool(true)
	apiKey := model.Settings.Get("apiKey").MustString()
	if apiKey == "" {
		return nil, alerting.ValidationError{Reason: "Could not find api key property in settings"}
	}

	return &OpsGenieNotifier{
		NotifierBase: NewNotifierBase(model.Id, model.IsDefault, model.Name, model.Type, model.Settings),
		ApiKey:       apiKey,
		AutoClose:    autoClose,
		log:          log.New("alerting.notifier.opsgenie"),
	}, nil
}

type OpsGenieNotifier struct {
	NotifierBase
	ApiKey    string
	AutoClose bool
	log       log.Logger
}

func (this *OpsGenieNotifier) Notify(evalContext *alerting.EvalContext) error {
	metrics.M_Alerting_Notification_Sent_OpsGenie.Inc(1)

	var err error
	switch evalContext.Rule.State {
	case m.AlertStateOK:
		if this.AutoClose {
			err = this.closeAlert(evalContext)
		}
	case m.AlertStateAlerting:
		err = this.createAlert(evalContext)
	}
	return err
}

func (this *OpsGenieNotifier) createAlert(evalContext *alerting.EvalContext) error {
	this.log.Info("Creating OpsGenie alert", "ruleId", evalContext.Rule.Id, "notification", this.Name)

	ruleUrl, err := evalContext.GetRuleUrl()
	if err != nil {
		this.log.Error("Failed get rule link", "error", err)
		return err
	}

	bodyJSON := simplejson.New()
	bodyJSON.Set("apiKey", this.ApiKey)
	bodyJSON.Set("message", evalContext.Rule.Name)
	bodyJSON.Set("source", "Grafana")
	bodyJSON.Set("alias", "alertId-"+strconv.FormatInt(evalContext.Rule.Id, 10))
	bodyJSON.Set("description", fmt.Sprintf("%s - %s\n%s", evalContext.Rule.Name, ruleUrl, evalContext.Rule.Message))

	details := simplejson.New()
	details.Set("url", ruleUrl)
	if evalContext.ImagePublicUrl != "" {
		details.Set("image", evalContext.ImagePublicUrl)
	}

	bodyJSON.Set("details", details)
	body, _ := bodyJSON.MarshalJSON()

	cmd := &m.SendWebhookSync{
		Url:        opsgenieCreateAlertURL,
		Body:       string(body),
		HttpMethod: "POST",
	}

	if err := bus.DispatchCtx(evalContext.Ctx, cmd); err != nil {
		this.log.Error("Failed to send notification to OpsGenie", "error", err, "body", string(body))
	}

	return nil
}

func (this *OpsGenieNotifier) closeAlert(evalContext *alerting.EvalContext) error {
	this.log.Info("Closing OpsGenie alert", "ruleId", evalContext.Rule.Id, "notification", this.Name)

	bodyJSON := simplejson.New()
	bodyJSON.Set("apiKey", this.ApiKey)
	bodyJSON.Set("alias", "alertId-"+strconv.FormatInt(evalContext.Rule.Id, 10))
	body, _ := bodyJSON.MarshalJSON()

	cmd := &m.SendWebhookSync{
		Url:        opsgenieCloseAlertURL,
		Body:       string(body),
		HttpMethod: "POST",
	}

	if err := bus.DispatchCtx(evalContext.Ctx, cmd); err != nil {
		this.log.Error("Failed to send notification to OpsGenie", "error", err, "body", string(body))
		return err
	}

	return nil
}