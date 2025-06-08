# FIXME:

def FIXME():
    """
    FIXME:
    """
    print("""
FIXME: This should *not* be linted.
""")

# XXX: This should be flagged!

NOTE = False # XXX:
XXX = True # NOTE:

r"""
NOTE:



XXX:
"""

def NOTE():
    '''
    NOTE:
    '''
    XXX = '''
XXX: This should *not* be linted.
'''

def foo(self):
    """NOTE This is the start of a block.

    TODO: Assume that a file is modified since an invalid timestamp as per RFC
    2616, section 14.25. GMT
    """
    invalid_date = 'FIXME: Mon, 28 May 999999999999 28:25:26 GMT'


# `FIXME`:
