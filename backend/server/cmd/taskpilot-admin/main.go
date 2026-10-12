// Command taskpilot-admin runs migrations and manages tenants and API keys.
//
//	taskpilot-admin migrate up|status
//	taskpilot-admin tenant create -name <name>
//	taskpilot-admin key create -tenant <uuid> -name <name> [-scopes jobs:read,jobs:write,jobs:cancel] [-expires 2160h]
//	taskpilot-admin key revoke -id <uuid>
//	taskpilot-admin tasktype list
//	taskpilot-admin tasktype retire -type <task_type> -version <n>
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/auth"
	"github.com/shuklaAlkesh/taskpilot/backend/server/internal/store"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: taskpilot-admin migrate up|status | tenant create | key create|revoke | tasktype list|retire")
	}
	cmd, flags := args[0]+" "+args[1], args[2:]
	switch cmd {
	case "migrate up", "migrate status":
		return migrate(ctx, args[1])
	case "tenant create":
		return createTenant(ctx, flags)
	case "key create":
		return createKey(ctx, flags)
	case "key revoke":
		return revokeKey(ctx, flags)
	case "tasktype list":
		return listTaskTypes(ctx)
	case "tasktype retire":
		return retireTaskType(ctx, flags)
	}
	return fmt.Errorf("unknown command %q", cmd)
}

// open connects with the URL in env var name (the owner URL for migrations, the app URL otherwise).
func open(ctx context.Context, name string) (*pgxpool.Pool, error) {
	url := os.Getenv(name)
	if url == "" {
		return nil, fmt.Errorf("%s: required", name)
	}
	return store.Open(ctx, url, 2)
}

func migrate(ctx context.Context, action string) error {
	pool, err := open(ctx, "TASKPILOT_MIGRATE_DATABASE_URL")
	if err != nil {
		return err
	}
	defer pool.Close()
	if action == "up" {
		results, err := store.Migrate(ctx, pool)
		for _, r := range results {
			fmt.Printf("applied %s (%s)\n", r.Source.Path, r.Duration.Round(time.Millisecond))
		}
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	current, target, err := store.MigrationStatus(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Printf("schema version %d (latest %d)\n", current, target)
	return nil
}

func createTenant(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tenant create", flag.ContinueOnError)
	name := fs.String("name", "", "tenant name (1-100 chars)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pool, err := open(ctx, "DATABASE_URL")
	if err != nil {
		return err
	}
	defer pool.Close()
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, id, *name); err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}
	fmt.Println("tenant id:", id)
	return nil
}

func createKey(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("key create", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id")
	name := fs.String("name", "", "key name (1-100 chars)")
	scopeList := fs.String("scopes", strings.Join(auth.AllScopes, ","), "comma-separated scopes")
	expires := fs.Duration("expires", 90*24*time.Hour, "lifetime; 0 = never expires (Q-SEC-2)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tenantID, err := uuid.Parse(*tenant)
	if err != nil {
		return errors.New("-tenant must be a UUID")
	}
	scopes := strings.Split(*scopeList, ",")
	for _, s := range scopes {
		if !slices.Contains(auth.AllScopes, s) {
			return fmt.Errorf("unknown scope %q", s)
		}
	}
	var expiresAt *time.Time
	if *expires > 0 {
		t := time.Now().Add(*expires)
		expiresAt = &t
	}
	pool, err := open(ctx, "DATABASE_URL")
	if err != nil {
		return err
	}
	defer pool.Close()
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	full, prefix, hash := auth.NewKey()
	if _, err := pool.Exec(ctx, `
		INSERT INTO api_keys (id, tenant_id, name, prefix, secret_hash, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, tenantID, *name, prefix, hash, scopes, expiresAt); err != nil {
		return fmt.Errorf("create key: %w", err)
	}
	fmt.Println("key id:", id)
	fmt.Println("api key (shown once, store it now):", full)
	return nil
}

func revokeKey(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("key revoke", flag.ContinueOnError)
	idFlag := fs.String("id", "", "key id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := uuid.Parse(*idFlag)
	if err != nil {
		return errors.New("-id must be a UUID")
	}
	pool, err := open(ctx, "DATABASE_URL")
	if err != nil {
		return err
	}
	defer pool.Close()
	tag, err := pool.Exec(ctx, `UPDATE api_keys SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("revoke key: %w", err)
	}
	fmt.Println("revoked:", tag.RowsAffected() == 1)
	return nil
}

// listTaskTypes prints every registered task type with its queue and how many workers can run it.
func listTaskTypes(ctx context.Context) error {
	pool, err := open(ctx, "DATABASE_URL")
	if err != nil {
		return err
	}
	defer pool.Close()
	rows, err := pool.Query(ctx, `
		SELECT t.task_type, t.task_version, t.queue, t.registered_by, t.updated_at,
			(SELECT count(*) FROM workers w WHERE w.last_seen_at > now() - interval '30 seconds'
				AND (t.task_type || '@' || t.task_version) = ANY(w.task_types))
		FROM task_types t ORDER BY t.task_type, t.task_version`)
	if err != nil {
		return fmt.Errorf("list task types: %w", err)
	}
	defer rows.Close()
	fmt.Printf("%-45s %-4s %-16s %-7s %s\n", "TASK TYPE", "VER", "QUEUE", "ONLINE", "LAST REGISTERED")
	for rows.Next() {
		var name, queue, by string
		var version, online int
		var at time.Time
		if err := rows.Scan(&name, &version, &queue, &by, &at, &online); err != nil {
			return fmt.Errorf("list task types: %w", err)
		}
		fmt.Printf("%-45s %-4d %-16s %-7d %s by %s\n", name, version, queue, online, at.Format(time.RFC3339), by)
	}
	return rows.Err()
}

// retireTaskType removes a task type version so new jobs are rejected. Remove the handler from
// the workers first, or the next worker start registers it again. Existing jobs keep their history.
func retireTaskType(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tasktype retire", flag.ContinueOnError)
	name := fs.String("type", "", "task type, e.g. analytics.tasks.compute_usecase_metrics")
	version := fs.Int("version", 1, "task version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pool, err := open(ctx, "DATABASE_URL")
	if err != nil {
		return err
	}
	defer pool.Close()
	var inFlight int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE task_type = $1 AND task_version = $2
		AND status IN ('queued', 'running')`, *name, *version).Scan(&inFlight); err != nil {
		return fmt.Errorf("check jobs: %w", err)
	}
	if inFlight > 0 {
		return fmt.Errorf("%d queued or running jobs still use %s v%d; let them finish or cancel them first", inFlight, *name, *version)
	}
	tag, err := pool.Exec(ctx, `DELETE FROM task_types WHERE task_type = $1 AND task_version = $2`, *name, *version)
	if err != nil {
		return fmt.Errorf("retire task type: %w", err)
	}
	fmt.Println("retired:", tag.RowsAffected() == 1, "(API replicas pick this up within 10 s)")
	return nil
}
