using Xunit;

namespace Grafana.Sigil.Tests;

public sealed class AuthConfigTests
{
    public static TheoryData<AuthConfig, string> InvalidGenerationAuthConfigs =>
        new()
        {
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.Tenant,
                },
                "generation auth mode 'tenant' requires tenant_id"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.Bearer,
                },
                "generation auth mode 'bearer' requires bearer_token"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.None,
                    TenantId = "tenant-a",
                },
                "generation auth mode 'none' does not allow tenant_id or bearer_token"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.Tenant,
                    TenantId = "tenant-a",
                    BearerToken = "token",
                },
                "generation auth mode 'tenant' does not allow bearer_token"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.Bearer,
                    TenantId = "tenant-a",
                    BearerToken = "token",
                },
                "generation auth mode 'bearer' does not allow tenant_id"
            },
            {
                new AuthConfig
                {
                    Mode = (ExportAuthMode)99,
                },
                "unsupported generation auth mode"
            },
        };

    public static TheoryData<AuthConfig, string> InvalidTraceAuthConfigs =>
        new()
        {
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.Tenant,
                },
                "trace auth mode 'tenant' requires tenant_id"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.Bearer,
                },
                "trace auth mode 'bearer' requires bearer_token"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.None,
                    TenantId = "tenant-a",
                },
                "trace auth mode 'none' does not allow tenant_id or bearer_token"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.Tenant,
                    TenantId = "tenant-a",
                    BearerToken = "token",
                },
                "trace auth mode 'tenant' does not allow bearer_token"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.Bearer,
                    TenantId = "tenant-a",
                    BearerToken = "token",
                },
                "trace auth mode 'bearer' does not allow tenant_id"
            },
            {
                new AuthConfig
                {
                    Mode = (ExportAuthMode)99,
                },
                "unsupported trace auth mode"
            },
        };

    [Theory]
    [MemberData(nameof(InvalidGenerationAuthConfigs))]
    public void Constructor_RejectsInvalidGenerationAuthConfig(AuthConfig auth, string expected)
    {
        var config = TestHelpers.TestConfig(new CapturingGenerationExporter());
        config.GenerationExport.Auth = auth;

        var error = Assert.Throws<ArgumentException>(() => new SigilClient(config));

        Assert.Contains(expected, error.Message);
    }

    [Theory]
    [MemberData(nameof(InvalidTraceAuthConfigs))]
    public void Constructor_RejectsInvalidTraceAuthConfig(AuthConfig auth, string expected)
    {
        var config = TestHelpers.TestConfig(new CapturingGenerationExporter());
        config.Trace.Auth = auth;

        var error = Assert.Throws<ArgumentException>(() => new SigilClient(config));

        Assert.Contains(expected, error.Message);
    }
}
