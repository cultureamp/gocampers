package log

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/cultureamp/glamplify/env"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	gcontext "github.com/cultureamp/glamplify/context"
	gerrors "github.com/go-errors/errors"
	perrors "github.com/pkg/errors"
)

const (
	errorSkipFrames = 4
)

// "github.com/pkg/errors" supports this interface for retrieving stack trace on an error
type stackTracer interface {
	StackTrace() perrors.StackTrace
}

// SystemValues represents values from the system environment
type SystemValues struct {
}

// DurationAsISO8601 return a time.Duration as string in ISA8601 format
func DurationAsISO8601(duration time.Duration) string {
	return fmt.Sprintf("P%gS", duration.Seconds())
}

func newSystemValues() *SystemValues {
	return &SystemValues{}
}

func (df SystemValues) getSystemValues(rsFields gcontext.RequestScopedFields, properties Fields, event string, severity string) Fields {
	fields := Fields{
		Time:     df.timeNow(RFC3339Milli),
		Event:    event,
		Resource: df.hostName(),
		Os:       df.targetOS(),
		Severity: severity,
		Loc:      df.getLocation(4),
	}
	fields = df.getMandatoryFields(rsFields, fields, properties)
	fields = df.getEnvFields(fields, properties)

	return fields
}

func (df SystemValues) getErrorValues(err error, fields Fields) Fields {
	errorMessage := strings.TrimSpace(err.Error())

	stats := &debug.GCStats{}
	stack := df.getErrorStackTrace(err)
	debug.ReadGCStats(stats)

	fields[Exception] = Fields{
		"error": errorMessage,
		"trace": stack,
		"gc_stats": Fields{
			"last_gc":        stats.LastGC,
			"num_gc":         stats.NumGC,
			"pause_total":    stats.PauseTotal,
			"pause_history":  stats.Pause,
			"pause_end":      stats.PauseEnd,
			"page_quantiles": stats.PauseQuantiles,
		},
	}

	return fields
}

func (df SystemValues) getErrorStackTrace(err error) string {
	// is it the standard google error type?
	var se *gerrors.Error
	if errors.As(err, &se) {
		return string(se.Stack())
	}

	// does it support a Stack interface?
	var ews stackTracer
	if errors.As(err, &ews) {
		return df.getStackTracer(ews)
	}

	// skip 4 frames that belong to glamplify
	return df.getCurrentStack(errorSkipFrames)
}

func (df SystemValues) getStackTracer(ews stackTracer) string {
	frames := ews.StackTrace()

	buf := bytes.Buffer{}
	for _, f := range frames {
		s := fmt.Sprintf("%+s:%d\n", f, f)
		buf.WriteString(s)
	}

	return buf.String()
}

func (df SystemValues) getCurrentStack(skip int) string {
	stack := make([]uintptr, gerrors.MaxStackDepth)
	length := runtime.Callers(skip, stack[:])
	stack = stack[:length]

	buf := bytes.Buffer{}
	for _, pc := range stack {
		frame := gerrors.NewStackFrame(pc)
		buf.WriteString(frame.String())
	}

	return buf.String()
}

func (df SystemValues) getEnvFields(fields Fields, properties Fields) Fields {
	fields = df.addEnvFieldIfMissing(Product, env.ProductEnv, fields, properties)
	fields = df.addEnvFieldIfMissing(App, env.AppNameEnv, fields, properties)
	fields = df.addEnvFieldIfMissing(Farm, env.AppFarmEnv, fields, properties)
	fields = df.addEnvFieldIfMissing(Farm, env.AppFarmLegacyEnv, fields, properties) // spec changed, delete this after a while: 14/09/2020 Mike
	fields = df.addEnvFieldIfMissing(AppVer, env.AppVerEnv, fields, properties)
	fields = df.addEnvFieldIfMissing(AwsRegion, env.AwsRegionEnv, fields, properties)
	fields = df.addEnvFieldIfMissing(AwsAccountID, env.AwsAccountIDEnv, fields, properties)

	return fields
}

func (df SystemValues) getMandatoryFields(rsFields gcontext.RequestScopedFields, fields Fields, properties Fields) Fields {
	fields = df.addMandatoryFieldIfMissing(TraceID, rsFields.TraceID, fields, properties)
	fields = df.addMandatoryFieldIfMissing(RequestID, rsFields.RequestID, fields, properties)
	fields = df.addMandatoryFieldIfMissing(CorrelationID, rsFields.CorrelationID, fields, properties)
	fields = df.addMandatoryFieldIfMissing(Customer, rsFields.CustomerAggregateID, fields, properties)
	fields = df.addMandatoryFieldIfMissing(User, rsFields.UserAggregateID, fields, properties)

	return fields
}

func (df SystemValues) addEnvFieldIfMissing(fieldName string, osVar string, fields Fields, properties Fields) Fields {
	// If it contains it already, all good!
	if _, ok := fields[fieldName]; ok {
		return fields
	}

	// If it is in the properties, then lift it out
	if fv, ok := properties[fieldName]; ok {
		fields[fieldName] = fv
		return fields
	}

	// otherwise get env value from OS
	prod := os.Getenv(osVar)
	fields[fieldName] = prod

	return fields
}

func (df SystemValues) addMandatoryFieldIfMissing(fieldName string, fieldValue string, fields Fields, properties Fields) Fields {
	// If it contains it already, all good!
	if _, ok := fields[fieldName]; ok {
		return fields
	}

	// If it is in the properties, then lift it out
	if fv, ok := properties[fieldName]; ok {
		fields[fieldName] = fv
		return fields
	}

	fields[fieldName] = fieldValue
	return fields
}

func (df SystemValues) timeNow(format string) string {
	return time.Now().UTC().Format(format)
}

func (df SystemValues) getLocation(caller int) string {
	pc, file, line, ok := runtime.Caller(caller)
	for ok && strings.Contains(file, "glamplify") {
		caller++
		pc, file, line, ok = runtime.Caller(caller)
	}
	if !ok {
		return "unknown:0:unknown"
	}

	fn := runtime.FuncForPC(pc)
	methodName := fn.Name()
	return fmt.Sprintf("%s:%d:%s", file, line, methodName)
}

var host string
var hostOnce sync.Once

func (df SystemValues) hostName() string {
	var err error
	hostOnce.Do(func() {
		host, err = os.Hostname()
		if err != nil {
			host = Unknown
		}
	})

	return host
}

func (df SystemValues) targetOS() string {
	return runtime.GOOS
}
