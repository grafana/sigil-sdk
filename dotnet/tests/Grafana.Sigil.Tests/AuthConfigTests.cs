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

    [Theory]
    [MemberData(nameof(InvalidGenerationAuthConfigs))]
    public void Constructor_RejectsInvalidGenerationAuthConfig(AuthConfig auth, string expected)
    {
        var config = TestHelpers.TestConfig(new CapturingGenerationExporter());
        config.GenerationExport.Auth = auth;

        var error = Assert.Throws<ArgumentException>(() => new SigilClient(config));

        Assert.Contains(expected, error.Message);
    }

    [Fact]
    public async Task Constructor_AppliesGenerationBearerHeaderFromAuth()
    {
        var config = TestHelpers.TestConfig(new CapturingGenerationExporter());
        config.GenerationExport.Auth = new AuthConfig
        {
            Mode = ExportAuthMode.Bearer,
            BearerToken = "token-a",
        };

        await using var client = new SigilClient(config);

        Assert.Equal("Bearer token-a", config.GenerationExport.Headers["Authorization"]);
    }

    [Fact]
    public async Task Constructor_PreservesExplicitGenerationAuthorizationHeader()
    {
        var config = TestHelpers.TestConfig(new CapturingGenerationExporter());
        config.GenerationExport.Headers["authorization"] = "Bearer override-token";
        config.GenerationExport.Auth = new AuthConfig
        {
            Mode = ExportAuthMode.Bearer,
            BearerToken = "token-from-auth",
        };

        await using var client = new SigilClient(config);

        Assert.Equal("Bearer override-token", config.GenerationExport.Headers["authorization"]);
    }

}
