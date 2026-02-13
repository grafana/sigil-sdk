namespace Grafana.Sigil;

public class SigilException : Exception
{
    public SigilException(string message)
        : base(message)
    {
    }

    public SigilException(string message, Exception inner)
        : base(message, inner)
    {
    }
}

public sealed class ValidationException : SigilException
{
    public ValidationException(string message)
        : base(message)
    {
    }
}

public class EnqueueException : SigilException
{
    public EnqueueException(string message)
        : base(message)
    {
    }

    public EnqueueException(string message, Exception inner)
        : base(message, inner)
    {
    }
}

public sealed class QueueFullException : EnqueueException
{
    public QueueFullException(string message)
        : base(message)
    {
    }
}

public sealed class ClientShutdownException : EnqueueException
{
    public ClientShutdownException(string message)
        : base(message)
    {
    }
}

public sealed class MappingException : SigilException
{
    public MappingException(string message)
        : base(message)
    {
    }

    public MappingException(string message, Exception inner)
        : base(message, inner)
    {
    }
}
