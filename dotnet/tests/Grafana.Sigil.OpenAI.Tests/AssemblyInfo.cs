using System.Runtime.CompilerServices;

internal static class TestEnvCleanup
{
    [ModuleInitializer]
    internal static void ClearSigilEnvVars()
    {
        // Clear canonical SIGIL_* env vars so individual tests don't pick up
        // developer-machine config when they construct a SigilClient.
        string[] keys =
        {
            "SIGIL_ENDPOINT",
            "SIGIL_PROTOCOL",
            "SIGIL_INSECURE",
            "SIGIL_HEADERS",
            "SIGIL_AUTH_MODE",
            "SIGIL_AUTH_TENANT_ID",
            "SIGIL_AUTH_TOKEN",
            "SIGIL_AGENT_NAME",
            "SIGIL_AGENT_VERSION",
            "SIGIL_USER_ID",
            "SIGIL_TAGS",
            "SIGIL_CONTENT_CAPTURE_MODE",
            "SIGIL_DEBUG",
        };
        foreach (var key in keys)
        {
            Environment.SetEnvironmentVariable(key, null);
        }
    }
}
