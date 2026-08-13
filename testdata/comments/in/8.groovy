#!/usr/bin/env groovy
// This is a line comment.
// It spans two lines.

/*
 * This is a block comment.
 * It also spans two lines.
 */

/**
 * This is a Groovydoc comment.
 *
 * With a second paragraph.
 */
class Example {
    static void main(String[] args) {
        println('Hello World') // A trailing comment.
    }
}

pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                sh 'make' /* An inline block comment. */
            }
        }
    }
}
