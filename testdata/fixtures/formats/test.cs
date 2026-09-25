// NOTE: a line comment
// XXX: continued on the next line
namespace Test
{
    /// <summary>TODO: an XML documentation comment.</summary>
    public class Test
    {
        /* NOTE: a block comment */
        private const string Raw = """
            // TODO: a raw string, not a comment
            """;

        public string Greet(string name)
        {
            var s = @"C:\temp\"; // FIXME: a comment after a verbatim string
            return s + name; // XXX: a trailing comment
        }
    }
}
