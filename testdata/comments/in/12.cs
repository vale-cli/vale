// A line comment
// continued on the next line.
namespace Demo
{
    /// <summary>An XML documentation comment.</summary>
    class Paths
    {
        static string Dir = @"C:\temp\"; /* A block comment after a verbatim string. */
        static string Raw = """
            // Not a comment: inside a raw string literal.
            """;
        static string Url = $"{Dir}// not a comment";
        // ReSharper disable once UnusedMember.Local
        static int x = 1; // A trailing comment.
    }
}
