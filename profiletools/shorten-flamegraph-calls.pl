#!/usr/bin/perl

use strict;

sub shorten_type_name {
    my ($type) = @_;
    # Convert (*Type).fn to Type.fn.
    if (my ($deref_type) = ($type =~ /^\(\*(.*)\)$/)) {
	$type = $deref_type;
    }

    if ($type eq 'folderBranchOps') {
	return 'fBrO';
    }
    if ($type eq 'folderBlockOps') {
	return 'fBlO';
    }

    return $type;
}

sub trim_to_function_name {
    my ($full_name) = @_;
    # Remove package name.
    my ($path, $pkg, $fn) = ($full_name =~ /^(.*\/)?([^.]*)\.(.*)$/);
    if (my ($type, $method) = ($fn =~ /^(.*?)\.(.*)$/)) {
	$type = shorten_type_name($type);
	return "$type.$method";
    } else {
	# Free function.
	return $fn;
    }
}

while (my $line = <STDIN>) {
    chomp($line);
    # Regexp taken from flamegraph.pl.
    my ($stack, $samples) = ($line =~ /^(.*)\s+?(\d+(?:\.\d*)?)$/);
    my @fns = split(';', $stack);
    my @filtered_fns = map { trim_to_function_name($_) } @fns;
    my $filtered_line = join(';', @filtered_fns) . " $samples";
    print "$filtered_line\n";
}
