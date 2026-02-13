using Xunit;

namespace Grafana.Sigil.Tests;

public sealed class DependencyBoundaryTests
{
    [Fact]
    public void CoreProject_DoesNotDependOnProviderSdkPackages()
    {
        var projectPath = Path.GetFullPath(
            Path.Combine(AppContext.BaseDirectory, "../../../../../src/Grafana.Sigil/Grafana.Sigil.csproj")
        );
        var content = File.ReadAllText(projectPath);

        Assert.DoesNotContain("PackageReference Include=\"OpenAI\"", content, StringComparison.OrdinalIgnoreCase);
        Assert.DoesNotContain("PackageReference Include=\"Anthropic\"", content, StringComparison.OrdinalIgnoreCase);
        Assert.DoesNotContain("PackageReference Include=\"Google.GenAI\"", content, StringComparison.OrdinalIgnoreCase);
    }
}
