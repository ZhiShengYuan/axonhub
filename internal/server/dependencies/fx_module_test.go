package dependencies

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/zhenzou/executors"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/datastorage"
	"github.com/looplj/axonhub/internal/ent/system"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/db"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func configEntWire(cfg db.ConfigDB) string {
	return "config:" + cfg.DSN
}

func logEntWire(cfg db.LogsDB) string {
	return "log:" + cfg.DSN
}

func TestConfigDBLogsDBDistinct(t *testing.T) {
	var _ func(db.ConfigDB) string = configEntWire
	var _ func(db.LogsDB) string = logEntWire

	cfgDB := db.Config{DSN: "config-dsn"}
	cfgLog := db.Config{DSN: "log-dsn"}

	r1 := configEntWire(db.ConfigDB{Config: cfgDB})
	r2 := logEntWire(db.LogsDB{Config: cfgLog})

	if r1 != "config:config-dsn" {
		t.Errorf("configEntWire: expected 'config:config-dsn', got %q", r1)
	}
	if r2 != "log:log-dsn" {
		t.Errorf("logEntWire: expected 'log:log-dsn', got %q", r2)
	}
}

func TestNewConfigEntClient(t *testing.T) {
	cfg := db.ConfigDB{
		Config: db.Config{
			Dialect: "sqlite3",
			DSN:     "file::memory:?cache=shared",
		},
	}

	client := db.NewConfigEntClient(cfg)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	defer client.Close()
}

func TestNewLogEntClient(t *testing.T) {
	cfg := db.LogsDB{
		Config: db.Config{
			Dialect: "sqlite3",
			DSN:     "file::memory:?cache=shared",
		},
	}

	client := db.NewLogEntClient(cfg)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	defer client.Close()
}

func TestDBLogsAbsentReusesConfigDBUnderFX(t *testing.T) {
	configDSN := sqliteDSN(t, "config")

	var out splitDBOutput
	app := newSplitDBApp(t, configDSN, "", &out)
	app.RequireStart()
	defer app.RequireStop()

	requireSameClient(t, out.ConfigClient, out.LogClient)

	ctx := authz.WithSystemBypass(context.Background(), "test-split-db-reuse")
	seedConfigOwnedRows(t, ctx, out.ConfigClient, "reuse")
	seedPrimaryDataStorage(t, ctx, out.ConfigClient, "reuse-primary")

	createLogOwnedRows(t, ctx, out, "reuse")

	requireCounts(t, ctx, out.ConfigClient, splitDBCounts{
		channels:     1,
		models:       1,
		apiKeys:      1,
		dataStorages: 2,
		requests:     1,
		usageLogs:    1,
	})
	requireCounts(t, ctx, out.LogClient, splitDBCounts{
		channels:     1,
		models:       1,
		apiKeys:      1,
		dataStorages: 2,
		requests:     1,
		usageLogs:    1,
	})
}

func TestDBLogsPresentSplitsRuntimeOwnershipUnderFX(t *testing.T) {
	configDSN := sqliteDSN(t, "config")
	logDSN := sqliteDSN(t, "log")

	var out splitDBOutput
	app := newSplitDBApp(t, configDSN, logDSN, &out)
	app.RequireStart()
	defer app.RequireStop()

	requireDifferentClient(t, out.ConfigClient, out.LogClient)

	ctx := authz.WithSystemBypass(context.Background(), "test-split-db-separate")
	seedConfigOwnedRows(t, ctx, out.ConfigClient, "split")
	seedPrimaryDataStorage(t, ctx, out.LogClient, "split-primary")

	createLogOwnedRows(t, ctx, out, "split")

	requireCounts(t, ctx, out.ConfigClient, splitDBCounts{
		channels: 1,
		models:   1,
		apiKeys:  1,
	})
	requireCounts(t, ctx, out.LogClient, splitDBCounts{
		dataStorages: 2,
		requests:     1,
		usageLogs:    1,
	})
}

func TestSplitDBDataMigrationsUseIndependentSystemVersionKeys(t *testing.T) {
	configDSN := sqliteDSN(t, "config")
	logDSN := sqliteDSN(t, "log")

	var out splitDBOutput
	app := newSplitDBApp(t, configDSN, logDSN, &out)
	app.RequireStart()
	defer app.RequireStop()

	ctx := authz.WithSystemBypass(context.Background(), "test-split-db-versions")
	markInitialized(t, ctx, out.ConfigClient)
	markInitialized(t, ctx, out.LogClient)

	if err := db.NewEntClientFor("config", db.Config{Dialect: "sqlite3", DSN: configDSN}, false).Close(); err != nil {
		t.Fatalf("rerun config migration close: %v", err)
	}
	if err := db.NewEntClientFor("log", db.Config{Dialect: "sqlite3", DSN: logDSN}, true).Close(); err != nil {
		t.Fatalf("rerun log migration close: %v", err)
	}

	configVersion, err := out.ConfigClient.System.Query().Where(system.KeyEQ(biz.SystemKeyVersion)).Only(ctx)
	if err != nil {
		t.Fatalf("config DB version lookup: %v", err)
	}

	logVersion, err := out.LogClient.System.Query().Where(system.KeyEQ(biz.SystemKeyVersion)).Only(ctx)
	if err != nil {
		t.Fatalf("log DB version lookup: %v", err)
	}

	if configVersion.Value == "" || logVersion.Value == "" {
		t.Fatalf("expected both DBs to have migration version keys, got config=%q log=%q", configVersion.Value, logVersion.Value)
	}
}

type splitDBOutput struct {
	ConfigClient       *ent.Client
	LogClient          *ent.Client
	ChannelService     *biz.ChannelService
	DataStorageService *biz.DataStorageService
	RequestService     *biz.RequestService
	UsageLogService    *biz.UsageLogService
}

type splitDBCounts struct {
	channels     int
	models       int
	apiKeys      int
	dataStorages int
	requests     int
	usageLogs    int
}

func newSplitDBApp(t *testing.T, configDSN, logDSN string, out *splitDBOutput) *fxtest.App {
	t.Helper()

	return fxtest.New(t,
		fx.Provide(func() db.Config { return db.Config{Dialect: "sqlite3", DSN: configDSN} }),
		fx.Provide(func() xcache.Config { return xcache.Config{Mode: xcache.ModeMemory} }),
		fx.Provide(func() *httpclient.HttpClient { return httpclient.NewHttpClient() }),
		fx.Provide(func() executors.ScheduledExecutor { return executors.NewPoolScheduleExecutor() }),
		fx.Provide(biz.NewLiveStreamRegistry),
		fx.Provide(biz.NewSystemService),
		fx.Provide(biz.NewWebhookNotifier),
		fx.Provide(biz.NewChannelService),
		fx.Provide(biz.NewDataStorageService),
		fx.Provide(biz.NewUsageLogService),
		fx.Provide(biz.NewRequestService),
		fx.Provide(fx.Annotate(configDBProvider, fx.ResultTags(`name:"config_ent"`))),
		fx.Provide(fx.Annotate(func(cfg db.Config, configClient biz.ConfigEntClient) *ent.Client {
			if logDSN == "" {
				return logDBProvider(cfg, db.Config{}, configClient.Client)
			}

			return logDBProvider(cfg, db.Config{Dialect: "sqlite3", DSN: logDSN}, configClient.Client)
		}, fx.ResultTags(`name:"log_ent"`))),
		fx.Invoke(func(configClient biz.ConfigEntClient, logClient biz.LogEntClient, channelService *biz.ChannelService, dataStorageService *biz.DataStorageService, requestService *biz.RequestService, usageLogService *biz.UsageLogService) {
			out.ConfigClient = configClient.Client
			out.LogClient = logClient.Client
			out.ChannelService = channelService
			out.DataStorageService = dataStorageService
			out.RequestService = requestService
			out.UsageLogService = usageLogService
		}),
	)
}

func sqliteDSN(t *testing.T, name string) string {
	t.Helper()
	return fmt.Sprintf("file:%s?cache=shared&_fk=0", filepath.Join(t.TempDir(), name+".db"))
}

func requireSameClient(t *testing.T, configClient, logClient *ent.Client) {
	t.Helper()
	if configClient != logClient {
		t.Fatalf("expected db_logs absent to reuse config ent client")
	}
}

func requireDifferentClient(t *testing.T, configClient, logClient *ent.Client) {
	t.Helper()
	if configClient == logClient {
		t.Fatalf("expected db_logs present to create separate log ent client")
	}
}

func seedConfigOwnedRows(t *testing.T, ctx context.Context, client *ent.Client, suffix string) {
	t.Helper()

	_, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("config-channel-"+suffix).
		SetStatus(channel.StatusEnabled).
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"split-model-" + suffix}).
		SetManualModels([]string{}).
		SetDefaultTestModel("split-model-"+suffix).
		Save(ctx)
	if err != nil {
		t.Fatalf("seed config channel: %v", err)
	}

	_, err = client.Model.Create().
		SetDeveloper("test").
		SetModelID("split-model-"+suffix).
		SetName("Split Model "+suffix).
		SetIcon("test").
		SetGroup("test").
		SetModelCard(&objects.ModelCard{}).
		SetSettings(&objects.ModelSettings{}).
		Save(ctx)
	if err != nil {
		t.Fatalf("seed config model: %v", err)
	}

	_, err = client.Project.Create().
		SetName("config-project-"+suffix).
		SetDescription("config owned project").
		Save(ctx)
	if err != nil {
		t.Fatalf("seed config project: %v", err)
	}

	_, err = client.APIKey.Create().
		SetProjectID(1).
		SetKey("sk-config-"+suffix).
		SetName("config api key "+suffix).
		Save(ctx)
	if err != nil {
		t.Fatalf("seed config api key: %v", err)
	}
}

func seedPrimaryDataStorage(t *testing.T, ctx context.Context, client *ent.Client, name string) {
	t.Helper()

	_, err := client.DataStorage.Create().
		SetName(name).
		SetDescription("primary database storage").
		SetType(datastorage.TypeDatabase).
		SetSettings(&objects.DataStorageSettings{}).
		SetPrimary(true).
		SetStatus(datastorage.StatusActive).
		Save(ctx)
	if err != nil {
		t.Fatalf("seed primary data storage: %v", err)
	}
}

func markInitialized(t *testing.T, ctx context.Context, client *ent.Client) {
	t.Helper()

	_, err := client.System.Create().
		SetKey(biz.SystemKeyInitialized).
		SetValue("true").
		Save(ctx)
	if err != nil {
		t.Fatalf("mark initialized: %v", err)
	}
}

func createLogOwnedRows(t *testing.T, ctx context.Context, out splitDBOutput, suffix string) {
	t.Helper()

	dataStorage, err := out.DataStorageService.CreateDataStorage(ent.NewContext(ctx, out.LogClient), &ent.CreateDataStorageInput{
		Name:        "log-storage-" + suffix,
		Description: "log owned storage",
		Type:        datastorage.TypeDatabase,
		Settings:    &objects.DataStorageSettings{},
	})
	if err != nil {
		t.Fatalf("create log data storage: %v", err)
	}

	requestCtx := contexts.WithProjectID(ctx, 1)
	requestCtx = ent.NewContext(requestCtx, out.LogClient)
	req, err := out.RequestService.CreateRequest(requestCtx, &llm.Request{
		Model: "split-model-" + suffix,
		Messages: []llm.Message{{
			Role:    "user",
			Content: llm.MessageContent{Content: stringPtr("hello")},
		}},
	}, &httpclient.Request{JSONBody: []byte(`{"messages":[{"role":"user","content":"hello"}],"model":"split-model"}`)}, llm.APIFormatOpenAIChatCompletion)
	if err != nil {
		t.Fatalf("create log request: %v", err)
	}
	if req.DataStorageID != 1 {
		t.Fatalf("expected request to use primary log DB data storage id 1, got %d (extra storage id %d)", req.DataStorageID, dataStorage.ID)
	}

	_, err = out.UsageLogService.CreateUsageLog(ctx, biz.CreateUsageLogParams{
		RequestID:     req.ID,
		ProjectID:     1,
		ChannelID:     1,
		ActualModelID: "split-model-" + suffix,
		Usage:         &llm.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
		Source:        usagelog.SourceAPI,
		Format:        string(llm.APIFormatOpenAIChatCompletion),
	})
	if err != nil {
		t.Fatalf("create usage log: %v", err)
	}
}

func requireCounts(t *testing.T, ctx context.Context, client *ent.Client, expected splitDBCounts) {
	t.Helper()

	actual := splitDBCounts{
		channels:     countChannels(t, ctx, client),
		models:       countModels(t, ctx, client),
		apiKeys:      countAPIKeys(t, ctx, client),
		dataStorages: countDataStorages(t, ctx, client),
		requests:     countRequests(t, ctx, client),
		usageLogs:    countUsageLogs(t, ctx, client),
	}

	if actual != expected {
		t.Fatalf("unexpected counts: got %+v want %+v", actual, expected)
	}
}

func countOrFail(t *testing.T, name string, count int, err error) int {
	t.Helper()
	if err != nil {
		t.Fatalf("count %s: %v", name, err)
	}

	return count
}

func countChannels(t *testing.T, ctx context.Context, client *ent.Client) int {
	t.Helper()
	count, err := client.Channel.Query().Count(ctx)
	return countOrFail(t, "channels", count, err)
}

func countModels(t *testing.T, ctx context.Context, client *ent.Client) int {
	t.Helper()
	count, err := client.Model.Query().Count(ctx)
	return countOrFail(t, "models", count, err)
}

func countAPIKeys(t *testing.T, ctx context.Context, client *ent.Client) int {
	t.Helper()
	count, err := client.APIKey.Query().Count(ctx)
	return countOrFail(t, "api keys", count, err)
}

func countDataStorages(t *testing.T, ctx context.Context, client *ent.Client) int {
	t.Helper()
	count, err := client.DataStorage.Query().Count(ctx)
	return countOrFail(t, "data storages", count, err)
}

func countRequests(t *testing.T, ctx context.Context, client *ent.Client) int {
	t.Helper()
	count, err := client.Request.Query().Count(ctx)
	return countOrFail(t, "requests", count, err)
}

func countUsageLogs(t *testing.T, ctx context.Context, client *ent.Client) int {
	t.Helper()
	count, err := client.UsageLog.Query().Count(ctx)
	return countOrFail(t, "usage logs", count, err)
}

func stringPtr(value string) *string {
	return &value
}
