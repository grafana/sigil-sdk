using System.Text;
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
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.None,
                    BasicUser = "user",
                },
                "generation auth mode 'none' does not allow credentials"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.None,
                    BasicPassword = "secret",
                },
                "generation auth mode 'none' does not allow credentials"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.Basic,
                },
                "generation auth mode 'basic' requires basic_password"
            },
            {
                new AuthConfig
                {
                    Mode = ExportAuthMode.Basic,
                    BasicPassword = "secret",
                },
                "generation auth mode 'basic' requires basic_user or tenant_id"
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

    [Fact]
    public async Task Constructor_AppliesBasicAuthWithTenantId()
    {
        var config = TestHelpers.TestConfig(new CapturingGenerationExporter());
        config.GenerationExport.Auth = new AuthConfig
        {
            Mode = ExportAuthMode.Basic,
            TenantId = "42",
            BasicPassword = "secret",
        };

        await using var client = new SigilClient(config);

        var expected = "Basic " + Convert.ToBase64String(Encoding.UTF8.GetBytes("42:secret"));
        Assert.Equal(expected, config.GenerationExport.Headers["Authorization"]);
        Assert.Equal("42", config.GenerationExport.Headers["X-Scope-OrgID"]);
    }

    [Fact]
    public async Task Constructor_AppliesBasicAuthWithExplicitUser()
    {
        var config = TestHelpers.TestConfig(new CapturingGenerationExporter());
        config.GenerationExport.Auth = new AuthConfig
        {
            Mode = ExportAuthMode.Basic,
            TenantId = "42",
            BasicUser = "probe-user",
            BasicPassword = "secret",
        };

        await using var client = new SigilClient(config);

        var expected = "Basic " + Convert.ToBase64String(Encoding.UTF8.GetBytes("probe-user:secret"));
        Assert.Equal(expected, config.GenerationExport.Headers["Authorization"]);
        Assert.Equal("42", config.GenerationExport.Headers["X-Scope-OrgID"]);
    }

    [Fact]
    public async Task Constructor_BasicAuthExplicitHeaderWins()
    {
        var config = TestHelpers.TestConfig(new CapturingGenerationExporter());
        config.GenerationExport.Headers["Authorization"] = "Basic override";
        config.GenerationExport.Headers["X-Scope-OrgID"] = "override-tenant";
        config.GenerationExport.Auth = new AuthConfig
        {
            Mode = ExportAuthMode.Basic,
            TenantId = "42",
            BasicPassword = "secret",
        };

        await using var client = new SigilClient(config);

        Assert.Equal("Basic override", config.GenerationExport.Headers["Authorization"]);
        Assert.Equal("override-tenant", config.GenerationExport.Headers["X-Scope-OrgID"]);
    }

}
