Feature: Checks
    Scenario: Script
        When I test "checks/Script"
        Then the output should contain exactly:
            """
            test.md:1:2:Scripts.CustomMsg:Some message
            test.md:29:1:Checks.ScriptRE:Consider inserting a new section heading at this point.
            test.md:39:1:Checks.ScriptRE:Consider inserting a new section heading at this point.
            """

    Scenario: Metric
        When I test "checks/Metric"
        Then the output should contain exactly:
            """
            test.md:1:1:Checks.MetricValue:This topic has 1.00 H2s in it.
            """

    Scenario: Conditional
        When I test "checks/Conditional"
        Then the output should contain exactly:
            """
            test.md:9:5:Checks.MultiCapture:'NFL' has no definition
            """

    Scenario: Occurrence
        When I test "checks/Occurrence"
        Then the output should contain exactly:
            """
            test.md:1:1:demo.ZeroOccurrence:No intro
            test.md:1:3:demo.MinCount:Content too short.
            test2.md:1:3:demo.MinCount:Content too short.
            test3.md:7:37:demo.CharCount:Topic titles should use fewer than 70 characters.
            test3.md:11:4:demo.CharCount:Topic titles should use fewer than 70 characters.
            test3.md:27:6:demo.CharCount:Topic titles should use fewer than 70 characters.
            """

    Scenario: SentenceCase
        When I test "checks/SentenceCase"
        Then the output should contain exactly:
            """
            test.html:6:1:demo.SentenceCase:'Something Weird is happening With Vale With Entities' should be sentence-cased
            test.md:5:3:demo.SentenceCase:'this isn't in sentence case' should be sentence-cased
            test.md:11:3:demo.SentenceCase:'This Does Not Comply' should be sentence-cased
            """

    Scenario: Repetition
        When I test "checks/Repetition"
        Then the output should contain exactly:
            """
            test.md:5:170:Vale.Repetition:'plan' is repeated!
            test.md:21:27:Vale.Repetition:'a' is repeated!
            test.md:23:56:Vale.Repetition:'completes' is repeated!
            test.md:25:130:Vale.Repetition:'registry' is repeated!
            test.md:27:1:Vale.Repetition:'Mermaid' is repeated!
            test.md:29:17:Vale.Repetition:'one' is repeated!
            test.md:31:6:Vale.Repetition:'to' is repeated!
            test.md:35:1:Vale.Repetition:'use' is repeated!
            text.rst:6:17:Vale.Repetition:'as' is repeated!
            text.rst:8:33:Vale.Repetition:'the' is repeated!
            text.rst:15:7:Vale.Repetition:'and' is repeated!
            text.rst:16:22:Vale.Repetition:'on' is repeated!
            text.rst:19:1:Vale.Repetition:'this' is repeated!
            text.rst:20:13:Vale.Repetition:'be' is repeated!
            """

    Scenario: Capitalization
        When I test "checks/Capitalization"
        Then the output should contain exactly:
            """
            test2.md:1:3:demo.CustomCap:'Section about the MacBook pro ultimate edition' should be in title case
            test4.md:9:4:demo.SentenceCase:'An unimportant Heading' should be sentence-cased
            test4.md:15:4:demo.SentenceCase:'B. An unimportant heading' should be sentence-cased
            """

    Scenario: Spelling
        When I test "checks/Spelling"
        Then the output should contain exactly:
            """
            test.md:16:1:Vale.Spelling:Did you really mean 'gitlab'?
            test.md:28:11:Vale.Spelling:Did you really mean 'typpo'?
            test.md:32:17:Vale.Spelling:Did you really mean 'gfm'?
            test.md:34:17:Vale.Spelling:Did you really mean 'remmark'?
            """

    Scenario: Existence
        When I test "checks/Existence"
        Then the output should contain exactly:
            """
            test.md:3:6:write-good.E-Prime:Avoid using "is"
            test.md:7:4:write-good.E-Prime:Avoid using "Here's"
            test.md:9:6:write-good.E-Prime:Avoid using "is"
            test.md:9:28:vale.Spacing:Use exactly one space between sentences and clauses. Check 'e.A' for spacing problems.
            """

    Scenario: Substitution
        When I test "checks/Substitution"
        Then the output should contain exactly:
            """
            test.md:5:5:Bugs.Newline:Use 'test' rather than 'linuxptp'.
            test.md:8:5:Bugs.Newline:Use 'test' rather than 'linuxptp'.
            test.md:10:1:Bugs.URLCtx:Use 'Term' instead of 'term'
            test.md:15:29:Bugs.MatchCase:Consider using 'human kind' instead of 'mankind'.
            test.md:17:1:Bugs.MatchCase:Consider using 'Human kind' instead of 'Mankind'.
            test.md:17:14:Bugs.URLCtx:Use 'Term' instead of 'term'
            test.md:19:5:Bugs.KeepCase:Use 'mutual TLS' rather than Mutual TLS.
            test.md:19:36:Bugs.KeepCase:Use 'vNIC' rather than VNIC.
            test.md:21:1:Bugs.TermCase:Use 'JavaScript' rather than Javascript.
            test.md:21:53:Bugs.TermCase:Use 'JavaScript' rather than javascript.
            test.md:23:1:Bugs.TermCase:Use 'iOS' rather than IOS.
            test.md:25:1:Bugs.SameCase:Use 'MPL 2.0' instead of 'mpl 2.0'
            test.md:27:1:Bugs.SameCase:Use 'MPL 2.0' instead of 'MPL2.0'
            test.md:31:15:Bugs.EmptyReplace:Use  instead of 'obvious'.
            test2.md:3:1:demo.CapSub:Use 'Change to the `/etc` directory' instead of 'Change into the `/etc` directory'.
            test2.md:7:1:demo.CapSub:Use 'Change to the `/home/user` directory' instead of 'Change into the `/home/user` directory'.
            test2.md:9:1:demo.CapSub:Use 'Change to the `/etc/X11` directory' instead of 'Change into the `/etc/X11` directory'.
            test2.md:11:6:Bugs.WrongExp:Use 'versus' instead of 'vs '.
            """

    Scenario: Sequence
        When I test "checks/Sequence"
        Then the output should contain exactly:
            """
            test.adoc:3:4:LanguageTool.WOULD_BE_JJ_VB:The infinitive 'write' after 'be' requries 'to'. Did you mean 'be great *to* write'?
            test.adoc:9:88:LanguageTool.WOULD_BE_JJ_VB:The infinitive 'come' after 'be' requries 'to'. Did you mean 'be available *to* come'?
            test.adoc:11:7:LanguageTool.AMBIG:Avoid ambiguous pronouns
            test.adoc:11:32:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.adoc:13:5:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.adoc:15:24:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.adoc:17:42:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.adoc:21:5:LanguageTool.APOS_ARE:Did you mean "endpoints" instead of "endpoint's"?
            test.md:3:4:LanguageTool.WOULD_BE_JJ_VB:The infinitive 'write' after 'be' requries 'to'. Did you mean 'be great *to* write'?
            test.md:9:88:LanguageTool.WOULD_BE_JJ_VB:The infinitive 'come' after 'be' requries 'to'. Did you mean 'be available *to* come'?
            test.md:11:7:LanguageTool.AMBIG:Avoid ambiguous pronouns
            test.md:11:32:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.md:13:5:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.md:15:24:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.md:17:42:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.md:21:5:LanguageTool.APOS_ARE:Did you mean "endpoints" instead of "endpoint's"?
            test.md:25:1:LanguageTool.Metadata:Use data and metadata as plural nouns.
            test.md:29:1:LanguageTool.Metadata:Use data and metadata as plural nouns.
            test.md:31:17:LanguageTool.ARE_USING:Use 'by using' instead of 'using' when it follows a noun for clarity and grammatical correctness.
            test.txt:3:4:LanguageTool.WOULD_BE_JJ_VB:The infinitive 'write' after 'be' requries 'to'. Did you mean 'be great *to* write'?
            test.txt:9:88:LanguageTool.WOULD_BE_JJ_VB:The infinitive 'come' after 'be' requries 'to'. Did you mean 'be available *to* come'?
            test.txt:11:7:LanguageTool.AMBIG:Avoid ambiguous pronouns
            test.txt:11:32:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.txt:13:5:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.txt:15:24:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.txt:17:42:LanguageTool.OF_ALL_TIMES:In this context, the idiom needs to be spelled 'of all time'.
            test.txt:21:5:LanguageTool.APOS_ARE:Did you mean "endpoints" instead of "endpoint's"?
            test.txt:25:1:LanguageTool.Metadata:Use data and metadata as plural nouns.
            test.txt:29:1:LanguageTool.Metadata:Use data and metadata as plural nouns.
            """
