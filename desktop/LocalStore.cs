using Microsoft.Data.Sqlite;
using System.Security.Cryptography;
using System.Text.Json;

namespace BuctCourseLocal;

internal record DueNotice(CourseTask Task, int Hours);

internal sealed class LocalStore : IDisposable
{
    private readonly SqliteConnection db;

    internal LocalStore(string directory)
    {
        db = new SqliteConnection(new SqliteConnectionStringBuilder
        {
            DataSource = Path.Combine(directory, "course.db"), ForeignKeys = true
        }.ToString());
        db.Open();
        try
        {
            using var sql = Command("""
                PRAGMA journal_mode = WAL;
                CREATE TABLE IF NOT EXISTS task (
                    id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('homework','test','todo')),
                    subject TEXT NOT NULL, title TEXT NOT NULL, details TEXT NOT NULL, url TEXT NOT NULL,
                    deadline INTEGER NOT NULL, completed INTEGER NOT NULL DEFAULT 0, listed INTEGER NOT NULL DEFAULT 1
                );
                CREATE TABLE IF NOT EXISTS sent_notice (
                    task_id TEXT NOT NULL REFERENCES task(id) ON DELETE CASCADE,
                    deadline INTEGER NOT NULL, hours INTEGER NOT NULL,
                    PRIMARY KEY (task_id, deadline, hours)
                );
                CREATE TABLE IF NOT EXISTS setting (key TEXT PRIMARY KEY, value INTEGER NOT NULL);
                CREATE TABLE IF NOT EXISTS secret (key TEXT PRIMARY KEY, value BLOB NOT NULL);
                CREATE TABLE IF NOT EXISTS task_resource (
                    task_id TEXT PRIMARY KEY REFERENCES task(id) ON DELETE CASCADE,
                    course_id TEXT NOT NULL, item_id TEXT NOT NULL, url TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS task_filter (
                    kind TEXT NOT NULL CHECK(kind IN ('task','course','todo')),
                    key TEXT NOT NULL, title TEXT NOT NULL, PRIMARY KEY (kind, key)
                );
                CREATE TABLE IF NOT EXISTS course_cache (key TEXT PRIMARY KEY, payload TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS courseflow_state (
                    id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL, payload TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS courseflow_notice (id TEXT PRIMARY KEY, sent_at INTEGER NOT NULL);
                """);
            sql.ExecuteNonQuery();
        }
        catch
        {
            db.Dispose();
            throw;
        }
    }

    internal List<CourseTask> Tasks(bool includeHidden = false)
    {
        var result = new List<CourseTask>();
        using (var sql = Command("""
            SELECT t.id, t.kind, t.subject, t.title, t.details, t.url, t.deadline, t.completed, r.course_id, r.item_id, r.url
            FROM task t LEFT JOIN task_resource r ON t.id = r.task_id
            WHERE t.listed = 1
            ORDER BY t.completed, t.deadline <= 0, t.deadline, t.title
            """))
        using (var reader = sql.ExecuteReader())
        {
            while (reader.Read())
                result.Add(new CourseTask(reader.GetString(0), reader.GetString(1), reader.GetString(2), reader.GetString(3),
                    reader.GetString(4), reader.GetString(5), reader.GetInt64(6), reader.GetBoolean(7), reader.IsDBNull(8) ? null :
                    new TaskResource(reader.GetString(8), reader.GetString(9), reader.GetString(10))));
        }
        return includeHidden ? result : VisibleTasks(result);
    }

    private List<CourseTask> VisibleTasks(List<CourseTask> tasks)
    {
        var filters = Filters().ToLookup(filter => filter.Kind, filter => filter.Key);
        var hidden = filters["task"].Concat(filters["todo"]).ToHashSet();
        var blocked = filters["course"].ToHashSet();
        return tasks.Where(task => !hidden.Contains(task.Id) &&
            (task.IsTodo || (!blocked.Contains(task.CourseKey) && !blocked.Contains($"name:{task.Subject}")))).ToList();
    }

    internal List<TaskFilter> Filters()
    {
        using var sql = Command("SELECT key,title,kind FROM task_filter ORDER BY kind,title");
        using var reader = sql.ExecuteReader();
        var result = new List<TaskFilter>();
        while (reader.Read())
            result.Add(new TaskFilter(reader.GetString(0), reader.GetString(1), reader.GetString(2)));
        return result;
    }

    internal void SetFilter(TaskFilter filter, bool enabled)
    {
        var query = enabled ? "INSERT OR REPLACE INTO task_filter VALUES ($kind,$key,$title)" :
            "DELETE FROM task_filter WHERE kind=$kind AND key=$key";
        using var sql = Command(query, null, ("$kind", filter.Kind), ("$key", filter.Key), ("$title", filter.Title));
        sql.ExecuteNonQuery();
    }

    internal T? CourseCache<T>(string key) where T : class
    {
        using var sql = Command("SELECT payload FROM course_cache WHERE key=$key", null, ("$key", key));
        return sql.ExecuteScalar() is string payload ? JsonSerializer.Deserialize<T>(payload, CourseService.Json) : null;
    }

    internal void SaveCourseCache<T>(string key, T items)
    {
        using var sql = Command("INSERT OR REPLACE INTO course_cache VALUES ($key,$payload)", null,
            ("$key", key), ("$payload", JsonSerializer.Serialize(items, CourseService.Json)));
        sql.ExecuteNonQuery();
    }

    internal CourseFlowSnapshot? ReadCourseFlow()
    {
        using var sql = Command("SELECT revision,payload FROM courseflow_state WHERE id=1");
        using var reader = sql.ExecuteReader();
        return reader.Read() ? new CourseFlowSnapshot(reader.GetInt64(0), reader.GetString(1)) : null;
    }

    internal bool SaveCourseFlow(CourseFlowSnapshot state, long? expected)
    {
        using var sql = expected.HasValue ? Command("UPDATE courseflow_state SET revision=$rev,payload=$json WHERE id=1 AND revision=$expected",
            null, ("$rev", state.Revision), ("$json", state.Json), ("$expected", expected.Value)) :
            Command("INSERT OR IGNORE INTO courseflow_state VALUES (1,$rev,$json)", null, ("$rev", state.Revision), ("$json", state.Json));
        return sql.ExecuteNonQuery() == 1;
    }

    internal bool HasCourseFlowNotice(string id)
    {
        using var sql = Command("SELECT 1 FROM courseflow_notice WHERE id=$id", null, ("$id", id));
        return sql.ExecuteScalar() is not null;
    }

    internal void MarkCourseFlowNotice(string id, long now)
    {
        using var sql = Command("DELETE FROM courseflow_notice WHERE sent_at < $cutoff; INSERT OR IGNORE INTO courseflow_notice VALUES ($id,$now)",
            null, ("$id", id), ("$now", now), ("$cutoff", now - 2 * 86400));
        sql.ExecuteNonQuery();
    }

    internal void SaveSnapshot(IEnumerable<CourseTask> tasks, IReadOnlyCollection<string> incompleteKinds, long now)
    {
        using var transaction = db.BeginTransaction();
        foreach (var kind in new[] { "homework", "test" }.Except(incompleteKinds))
        {
            using var hide = Command("UPDATE task SET listed=0 WHERE kind=$kind", transaction, ("$kind", kind));
            hide.ExecuteNonQuery();
        }
        foreach (var task in tasks)
            Upsert(task, transaction, true);
        if (incompleteKinds.Count == 0)
            SetSetting("last_sync", now, transaction);
        transaction.Commit();
    }

    internal void SaveTodo(CourseTask task) => Upsert(task);

    internal void ImportTasks(IEnumerable<CourseTask> tasks)
    {
        using var transaction = db.BeginTransaction();
        foreach (var task in tasks)
        {
            using var existing = Command("""
                SELECT 1 FROM task WHERE id=$id OR
                    (kind <> 'todo' AND kind=$kind AND subject=$subject AND title=$title AND details=$details)
                LIMIT 1
                """, transaction, ("$id", task.Id), ("$kind", task.Kind), ("$subject", task.Subject),
                ("$title", task.Title), ("$details", task.Details));
            if (existing.ExecuteScalar() is null)
                Upsert(task, transaction);
        }
        transaction.Commit();
    }

    private void Upsert(CourseTask task, SqliteTransaction? transaction = null, bool matchLegacy = false)
    {
        var completed = task.Completed;
        if (matchLegacy)
        {
            using var lookup = Command("""
                SELECT completed FROM task
                WHERE id LIKE 'legacy:%' AND kind=$kind AND subject=$subject AND title=$title
                ORDER BY details = $details DESC LIMIT 1
                """, transaction, ("$kind", task.Kind), ("$subject", task.Subject), ("$title", task.Title), ("$details", task.Details));
            completed = lookup.ExecuteScalar() is 1L;
        }
        using var sql = Command("""
            INSERT INTO task (id,kind,subject,title,details,url,deadline,completed)
            VALUES ($id,$kind,$subject,$title,$details,$url,$deadline,$completed)
            ON CONFLICT(id) DO UPDATE SET subject=excluded.subject, title=excluded.title,
                details=excluded.details, url=excluded.url, deadline=excluded.deadline, listed=1
            """, transaction, ("$id", task.Id), ("$kind", task.Kind), ("$subject", task.Subject), ("$title", task.Title),
            ("$details", task.Details), ("$url", task.Url), ("$deadline", task.Deadline), ("$completed", completed));
        sql.ExecuteNonQuery();
        if (task.Resource is { } resource)
        {
            using var save = Command("INSERT OR REPLACE INTO task_resource VALUES ($id,$course,$item,$url)", transaction,
                ("$id", task.Id), ("$course", resource.CourseId), ("$item", resource.ItemId), ("$url", resource.Url));
            save.ExecuteNonQuery();
        }
    }

    internal void SetCompleted(string id, bool value)
    {
        using var sql = Command("UPDATE task SET completed=$value WHERE id=$id", null, ("$value", value), ("$id", id));
        sql.ExecuteNonQuery();
    }

    internal long Setting(string key, long fallback = 0)
    {
        using var sql = Command("SELECT value FROM setting WHERE key=$key", null, ("$key", key));
        return sql.ExecuteScalar() as long? ?? fallback;
    }

    internal void SetSetting(string key, long value, SqliteTransaction? transaction = null)
    {
        using var sql = Command("""
            INSERT INTO setting VALUES ($key,$value) ON CONFLICT(key) DO UPDATE SET value=excluded.value
            """, transaction, ("$key", key), ("$value", value));
        sql.ExecuteNonQuery();
    }

    internal T? ReadSecret<T>(string key) where T : class
    {
        using var sql = Command("SELECT value FROM secret WHERE key=$key", null, ("$key", key));
        if (sql.ExecuteScalar() is not byte[] bytes)
            return null;
        var clear = ProtectedData.Unprotect(bytes, null, DataProtectionScope.CurrentUser);
        try { return JsonSerializer.Deserialize<T>(clear, CourseService.Json); }
        finally { CryptographicOperations.ZeroMemory(clear); }
    }

    internal void SaveSecret<T>(string key, T value, SqliteTransaction? transaction = null)
    {
        var clear = JsonSerializer.SerializeToUtf8Bytes(value, CourseService.Json);
        try
        {
            var encrypted = ProtectedData.Protect(clear, null, DataProtectionScope.CurrentUser);
            using var sql = Command("""
                INSERT INTO secret VALUES ($key,$value) ON CONFLICT(key) DO UPDATE SET value=excluded.value
                """, transaction, ("$key", key), ("$value", encrypted));
            sql.ExecuteNonQuery();
        }
        finally { CryptographicOperations.ZeroMemory(clear); }
    }

    internal void EndSession(bool clearCourseData)
    {
        using var transaction = db.BeginTransaction();
        using var secrets = Command("DELETE FROM secret WHERE key IN ('account','cookies')", transaction);
        secrets.ExecuteNonQuery();
        if (clearCourseData)
        {
            using var course = Command("""
                DELETE FROM task WHERE kind <> 'todo';
                DELETE FROM setting WHERE key='last_sync';
                DELETE FROM task_filter WHERE kind <> 'todo';
                DELETE FROM course_cache WHERE key <> 'schedule';
                """, transaction);
            course.ExecuteNonQuery();
        }
        transaction.Commit();
    }

    internal List<DueNotice> DueNotices(long now)
    {
        var result = new List<DueNotice>();
        foreach (var task in Tasks())
        {
            var remaining = task.Deadline - now;
            if (task.Completed || remaining <= 0 || remaining > 24 * 3600)
                continue;
            var hours = remaining <= 3600 ? 1 : remaining <= 12 * 3600 ? 12 : 24;
            using var sent = Command("SELECT 1 FROM sent_notice WHERE task_id=$id AND deadline=$due AND hours=$hours", null,
                ("$id", task.Id), ("$due", task.Deadline), ("$hours", hours));
            if (sent.ExecuteScalar() is null)
                result.Add(new DueNotice(task, hours));
        }
        return result;
    }

    internal void MarkNotified(IEnumerable<DueNotice> notices)
    {
        using var transaction = db.BeginTransaction();
        foreach (var notice in notices)
        {
            using var sql = Command("INSERT OR IGNORE INTO sent_notice VALUES ($id,$due,$hours)", transaction,
                ("$id", notice.Task.Id), ("$due", notice.Task.Deadline), ("$hours", notice.Hours));
            sql.ExecuteNonQuery();
        }
        transaction.Commit();
    }

    private SqliteCommand Command(string query, SqliteTransaction? transaction = null, params (string Name, object Value)[] parameters)
    {
        var sql = db.CreateCommand();
        sql.Transaction = transaction;
        sql.CommandText = query;
        foreach (var (name, value) in parameters)
            sql.Parameters.AddWithValue(name, value);
        return sql;
    }

    public void Dispose() => db.Dispose();
}
